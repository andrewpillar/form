package form

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/schema"
	"github.com/gorilla/sessions"
)

type (
	Decoder   = schema.Decoder
	Converter = schema.Converter
)

// Options are the decoder options that will be configured on the Decoder when
// decoding the request data. This will be configured via the [Option] function.
type Options struct {
	AliasTag          string
	IgnoreUnknownKeys bool
	MaxSize           int
	Converters        map[any]Converter
	ZeroEmpty         bool
}

// Decoder returns the Decoder configured with the options from the underlying
// options set.
func (o *Options) Decoder() *Decoder {
	dec := schema.NewDecoder()
	dec.SetAliasTag(o.AliasTag)
	dec.IgnoreUnknownKeys(o.IgnoreUnknownKeys)
	dec.MaxSize(o.MaxSize)
	dec.ZeroEmpty(o.ZeroEmpty)

	for v, fn := range o.Converters {
		dec.RegisterConverter(v, fn)
	}
	return dec
}

type Option func(opts *Options)

// AliasTag specifies the struct tag alias that should be used for mapping
// data to the struct fields. The default for this is "schema".
func AliasTag(tag string) Option {
	return func(opts *Options) {
		opts.AliasTag = tag
	}
}

// IgnoreUnknownKeys will ignore any keys that exist in the request data but do
// not exist in the struct being decoded into.
func IgnoreUnknownKeys() Option {
	return func(opts *Options) {
		opts.IgnoreUnknownKeys = true
	}
}

// MaxSize configures the size of slices for URL nested arrays or object arrays.
func MaxSize(size int) Option {
	return func(opts *Options) {
		opts.MaxSize = size
	}
}

// RegisterConverter registers a converter function for a custom type.
func RegisterConverter(val any, fn Converter) Option {
	return func(opts *Options) {
		if opts.Converters == nil {
			opts.Converters = make(map[any]Converter)
		}
		opts.Converters[val] = fn
	}
}

// ZeroEmpty configures the behavior when decoding empty values into a map. If
// true then the zero vlaue is set in the map, otherwise empty values are
// ignored and do not change the value being mapped into.
func ZeroEmpty(z bool) Option {
	return func(opts *Options) {
		opts.ZeroEmpty = z
	}
}

// Form represents a form into which request data has been unmarshalled into.
// It wraps the Fields and Validate methods.
//
// Fields returns the [Fields] type which should contain the fields of the form.
// This value is used for flashing data into the session.
//
// Validate validates the form data itself. This should return the [Errors] type
// containing the errors, if any, that occurred during validation and for which
// field.
type Form interface {
	Fields() Fields

	Validate(ctx context.Context) error
}

// Fields represents the fields in the form. This is the value that is stored
// in the session data when a form is flashed to the session. This should not
// contain sensitive data, such as passwords.
type Fields map[string]string

// Get returns the value of the named value from the fields. If not found this
// returns an empty string.
func (f Fields) Get(name string) string {
	if f == nil {
		return ""
	}

	val, ok := f[name]

	if !ok {
		return ""
	}
	return val
}

func (f Fields) String() string {
	vals := make(url.Values)

	for k, v := range f {
		vals.Add(k, v)
	}
	return vals.Encode()
}

// Rule is used for validating field values in a form.
type Rule func(ctx context.Context, val any) error

var ErrFieldRequired = errors.New("field is required")

// Required checks to see if the given value was provided. If the value is a
// string, or returns a string via a call to String, then it will determine
// presence by whether or not that value is empty. If the value is a pointer
// then it is checked for nilness.
func Required(_ context.Context, val any) error {
	switch v := val.(type) {
	case string:
		if v == "" {
			return ErrFieldRequired
		}
	case interface{ String() string }:
		if v.String() == "" {
			return ErrFieldRequired
		}
	default:
		rv := reflect.ValueOf(val)

		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				return ErrFieldRequired
			}
		}
	}
	return nil
}

func errTypeAssert(from, to any) error {
	return fmt.Errorf("validate: cannot type assert %T to %T", from, to)
}

type MatchError struct {
	*regexp.Regexp
}

func (e MatchError) Is(target error) bool {
	var match MatchError

	if errors.As(target, &match) {
		return e.Regexp.String() == match.String()
	}
	return false
}

func (e MatchError) Error() string {
	return fmt.Sprintf("does not match pattern %q", e.Regexp.String())
}

// Matches returns a rule that will check if the given form values matches the
// given pattern. This errors if the underlying form value is not of type
// string.
func Matches(re *regexp.Regexp) Rule {
	return func(_ context.Context, val any) error {
		s, ok := val.(string)

		if !ok {
			return errTypeAssert(val, s)
		}

		if !re.Match([]byte(s)) {
			return MatchError{
				Regexp: re,
			}
		}
		return nil
	}
}

var reEmail = regexp.MustCompile("@")

var ErrEmailInvalid = errors.New("not a valid address")

// Email checks to see if the given value is an email address. This does a very
// simple pattern match to check for the presence of "@" in the string. Further
// validation of the email address itself should be done through the sending of
// a verification email.
func Email(ctx context.Context, val any) error {
	if err := Matches(reEmail)(ctx, val); err != nil {
		if _, ok := err.(MatchError); ok {
			return ErrEmailInvalid
		}
		return err
	}
	return nil
}

type LengthError struct {
	Min int
	Max int
}

func (e LengthError) Is(target error) bool {
	var length LengthError

	if errors.As(target, &length) {
		return length.Min == e.Min && length.Max == e.Max
	}
	return false
}

func (e LengthError) Error() string {
	return fmt.Sprintf("must be between %d and %d characters in length", e.Min, e.Max)
}

// Length returns a [Rule] that checks to ensure the form value is between the
// min and max length in size. If the form value is not of type string then
// this errors. If the value falls outside the given bounds then the error type
// of [LengthError] is returned.
func Length(min, max int) Rule {
	return func(_ context.Context, val any) error {
		s, ok := val.(string)

		if !ok {
			return errTypeAssert(val, s)
		}

		if l := len(s); l < min || l > max {
			return LengthError{
				Min: min,
				Max: max,
			}
		}
		return nil
	}
}

type FieldEqualsError string

func (e FieldEqualsError) Error() string {
	return "does not match " + string(e)
}

// Equals returns a [Rule] that checks to ensure the form value matches the
// given expected value. The given field is the name of the other form field the
// form value should match. This field value is returned as the
// [FieldEqualsError] type.
func Equals(field string, expected any) Rule {
	return func(_ context.Context, val any) error {
		if val != expected {
			return FieldEqualsError(field)
		}
		return nil
	}
}

// Errors contains the errors that occurred during the validation of a form.
// Each key in the map will be the field for which the errors occurred.
type Errors map[string][]error

// First returns the first error that occurred for the named field. This
// returns nil if there is no error.
func (e Errors) First(field string) error {
	if e == nil {
		return nil
	}

	if errs, ok := e[field]; ok {
		return errs[0]
	}
	return nil
}

// Get the errors that occurred for the named field. This returns nil if there
// are no errors.
func (e Errors) Get(field string) []error {
	if e == nil {
		return nil
	}

	arr, ok := e[field]

	if !ok {
		return nil
	}
	return arr
}

// Add an error to the named field.
func (e Errors) Add(field string, err error) {
	var empty schema.EmptyFieldError

	if errors.As(err, &empty) {
		err = ErrFieldRequired
	}

	if _, ok := e[field]; !ok {
		e[field] = make([]error, 0)
	}
	e[field] = append(e[field], err)
}

// Merge the given errors into the underlying set of errors. This will not merge
// in duplicates. That is, if an error exists in the given set being merged, and
// also exists in the underlying set, then it will not be added.
func (e Errors) Merge(errs Errors) {
	for fld, arr := range errs {
		set := make(map[string]struct{})

		for _, err := range e[fld] {
			set[err.Error()] = struct{}{}
		}

		for _, err := range arr {
			if _, ok := set[err.Error()]; !ok {
				e[fld] = append(e[fld], err)
			}
		}
	}
}

// Is checks to see if the given target is the same as the underlying error
// set.
func (e Errors) Is(target error) bool {
	errs := make(Errors)

	if errors.As(target, &errs) {
		if len(errs) != len(e) {
			return false
		}

		for k, v1 := range e {
			v2, ok := errs[k]

			if !ok {
				return false
			}

			if len(v1) != len(v2) {
				return false
			}

			for i := range v1 {
				if v1[i].Error() != v2[i].Error() {
					return false
				}
			}
		}
		return true
	}
	return false
}

// Error returns the string representation of the error set.
func (e Errors) Error() string {
	if len(e) == 0 {
		return ""
	}

	flds := make([]string, 0, len(e))
	sum := 0

	for k, v := range e {
		flds = append(flds, k)
		sum += len(v)
	}

	sort.Strings(flds)

	fld := flds[0]
	s := fld + " " + e.First(fld).Error()

	switch sum {
	case 1:
		return s
	case 2:
		return s + " (and 1 other error)"
	}
	return fmt.Sprintf("%s (and %d other errors)", s, sum-1)
}

func (e Errors) MarshalJSON() ([]byte, error) {
	m := make(map[string][]string, len(e))

	for k, v := range e {
		for _, err := range v {
			m[k] = append(m[k], err.Error())
		}
	}
	return json.Marshal(m)
}

func (e Errors) UnmarshalJSON(b []byte) error {
	m := make(map[string][]string)

	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	for k, v := range m {
		for _, s := range v {
			e[k] = append(e[k], errors.New(s))
		}
	}
	return nil
}

type field struct {
	name  string
	val   any
	rules []Rule
}

// Validator is used for validating form fields.
type Validator struct {
	fields []*field
}

// Add a set of rules for validating the form value of the given name.
func (v *Validator) Add(name string, val any, rules ...Rule) {
	if v.fields == nil {
		v.fields = make([]*field, 0)
	}

	v.fields = append(v.fields, &field{
		name:  name,
		val:   val,
		rules: rules,
	})
}

// Validate will iterate over all of the fields added to the validator and
// perform each rule for each field. All errors will be aggregated into the
// [Errors] type which will be returned if any errors did occur.
func (v *Validator) Validate(ctx context.Context) error {
	errs := make(Errors)

	for _, fld := range v.fields {
		for _, rule := range fld.rules {
			if err := rule(ctx, fld.val); err != nil {
				errs.Add(fld.name, err)
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

type DecodeError reflect.Kind

func (e DecodeError) Error() string {
	return fmt.Sprintf("cannot decode into %v", reflect.Kind(e))
}

// unmarshalFiles parses the files uploaded as part of the given request from
// the given field. This expects the given request to have had
// ParseMultipartForm called on it.
func unmarshalFiles(field string, r *http.Request) ([]*File, error) {
	hdrs, ok := r.MultipartForm.File[field]

	if !ok {
		return nil, nil
	}

	files := make([]*File, 0, len(hdrs))
	buf := make([]byte, 512)

	for _, hdr := range hdrs {
		f, err := hdr.Open()

		if err != nil {
			return nil, err
		}

		n, err := f.Read(buf)

		if err != nil {
			return nil, err
		}

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}

		files = append(files, &File{
			File:    f,
			hdr:     hdr,
			typ:     http.DetectContentType(buf[:n]),
			modTime: time.Now().UTC(),
		})
	}
	return files, nil
}

const (
	defaultAliasTag       = "schema"
	fileTypeName          = "File"
	maxMemory       int64 = 32 << 20
)

// Unmarshal parses the given request and unmarshals the data into the given
// form. If the given request has the Content-Type of application/json, then it
// will be decoded as such. If the given request has the Content-Type of
// multipart/form-data, then [net.http.Request.ParseMultipartForm] is called,
// otherwise [net.http.Request.ParseForm] is called.
func Unmarshal(r *http.Request, f Form, opts ...Option) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			if err := json.NewDecoder(r.Body).Decode(f); err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
			}
		}
		return nil
	}

	options := Options{
		AliasTag: defaultAliasTag,
	}

	for _, opt := range opts {
		opt(&options)
	}

	if r.Form == nil {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(maxMemory); err != nil {
				return err
			}

			rv := reflect.ValueOf(f)

			if kind := rv.Kind(); kind != reflect.Ptr || rv.IsNil() {
				return DecodeError(kind)
			}

			el := rv.Elem()
			rt := el.Type()

			for i := 0; i < el.NumField(); i++ {
				sf := rt.Field(i)
				sv := el.Field(i)

				kind := sf.Type.Kind()

				switch kind {
				case reflect.Slice:
					el := sf.Type.Elem()

					if el.Kind() != reflect.Ptr {
						continue
					}

					if el := el.Elem(); el.Name() != fileTypeName {
						continue
					}
				case reflect.Ptr:
					el := sf.Type.Elem()

					if el.Name() != fileTypeName {
						continue
					}
				default:
					continue
				}

				field := sf.Name

				if v := sf.Tag.Get(options.AliasTag); v != "" {
					if parts := strings.Split(v, ","); len(parts) > 0 {
						field = parts[0]
					}
				}

				ff, err := unmarshalFiles(field, r)

				if err != nil {
					return err
				}

				if kind == reflect.Ptr {
					if len(ff) > 0 {
						sv.Set(reflect.ValueOf(ff[0]))
					}
					continue
				}
				sv.Set(reflect.ValueOf(ff))
			}
		} else {
			if err := r.ParseForm(); err != nil {
				return err
			}
		}
	}

	dec := options.Decoder()

	if err := dec.Decode(f, r.Form); err != nil {
		multi := make(schema.MultiError)

		if errors.As(err, &multi) {
			errs := make(Errors)

			for k, err := range multi {
				errs.Add(k, err)
			}
			return errs
		}
		return err
	}
	return nil
}

// UnmarshalAndValidate unmarshal the given request into the given Form and
// validates it, returning any errors from the underlying Validate call.
func UnmarshalAndValidate(r *http.Request, f Form, opts ...Option) error {
	errs := make(Errors)

	if err := Unmarshal(r, f, opts...); err != nil {
		unmarshal := &json.UnmarshalTypeError{}

		switch {
		case errors.As(err, &errs):
		case errors.As(err, &unmarshal):
			errs.Add(unmarshal.Field, unmarshal)
		default:
			return err
		}
	}

	err := f.Validate(r.Context())

	if err != nil {
		errs2 := make(Errors)

		if errors.As(err, &errs2) {
			errs2.Merge(errs)
		}
	}
	return err
}

// File is a wrapper around the underlying [mime.multipart.File] interface.
// This also implements the [io.fs.FileInfo] interface. This represents a
// multipart file that has been sent in a request.
type File struct {
	multipart.File

	hdr     *multipart.FileHeader
	typ     string
	modTime time.Time
}

var _ fs.File = (*File)(nil)

func (f *File) Stat() (fs.FileInfo, error) { return f, nil }

func (f *File) Name() string       { return f.hdr.Filename }
func (f *File) Size() int64        { return f.hdr.Size }
func (f *File) Mode() fs.FileMode  { return fs.FileMode(0400) }
func (f *File) ModTime() time.Time { return f.modTime }
func (f *File) IsDir() bool        { return false }
func (f *File) Sys() any           { return nil }
func (f *File) MimeType() string   { return f.typ }

func (f *File) MarshalJSON() ([]byte, error) {
	smode := fmt.Sprintf("%o", f.Mode())
	mode, _ := strconv.Atoi(smode)

	return json.Marshal(map[string]any{
		"name":      f.Name(),
		"size":      f.Size(),
		"mime_type": f.MimeType(),
		"mode":      mode,
		"mod_time":  f.ModTime().Truncate(time.Second).UTC(),
	})
}

const (
	formKey = "form.Fields"
	errsKey = "form.Errors"
)

// Old unmarshals the previous Form and Errors that could be found in the
// given session. If found, they will be deleted from the given session.
func Old(sess *sessions.Session, f Form, errs Errors) {
	if val, ok := sess.Values[formKey]; ok {
		delete(sess.Values, formKey)

		vals, _ := url.ParseQuery(val.(string))

		dec := schema.NewDecoder()
		dec.Decode(f, vals)
	}

	if val, ok := sess.Values[errsKey]; ok {
		delete(sess.Values, errsKey)

		s := val.(string)
		json.Unmarshal([]byte(s), &errs)
	}
}

// Flash stores the given Form and Errors to the given session, which can
// then be retrieved via a subsequent call to Old.
func Flash(s *sessions.Session, f Form, errs Errors) {
	if f != nil {
		s.Values[formKey] = f.Fields().String()
	}

	if errs != nil {
		b, _ := json.Marshal(errs)
		s.Values[errsKey] = string(b)
	}
}
