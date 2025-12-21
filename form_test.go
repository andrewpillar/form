package form

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/schema"
	"github.com/gorilla/sessions"
)

type F struct {
	String  string  `schema:"string,required" json:"string"`
	Pattern string  `schema:"pattern,required" json:"pattern"`
	Int     int64   `schema:"int,required" json:"int"`
	Bool    *bool   `schema:"bool,required" json:"bool"`
	Secret  string  `schema:"secret" json:"secret"`
	File    *File   `schema:"file"`
	Files   []*File `schema:"files"`
}

func (f *F) Fields() Fields {
	return Fields{
		"string":  f.String,
		"pattern": f.Pattern,
		"int":     fmt.Sprintf("%v", f.Int),
		"bool":    fmt.Sprintf("%v", f.Bool),
	}
}

var rePattern = regexp.MustCompile("^[a-zA-Z]+$")

func (f *F) Validate(ctx context.Context) error {
	var v Validator

	v.Add("string", f.String, Required, Length(6, 60))
	v.Add("pattern", f.String, Required, Matches(rePattern))
	v.Add("int", f.Int, Required)
	v.Add("bool", f.Bool, Required)

	if f.Secret != "" {
		v.Add("secret", f.Secret, Equals("string", f.String))
	}
	return v.Validate(ctx)
}

const multipartContentBoundary = "formMultipartBody"

func multipartBody(t *testing.T, field string, n int, size int64, vals url.Values) io.Reader {
	t.Helper()

	var buf bytes.Buffer

	mpw := multipart.NewWriter(&buf)
	mpw.SetBoundary(multipartContentBoundary)

	for k := range vals {
		mpw.WriteField(k, vals.Get(k))
	}

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file.%d", i)

		w, err := mpw.CreateFormFile(field, name)

		if err != nil {
			t.Fatalf("mpw.CreateFormFile(%q, %q): %v\n", field, name, err)
		}

		_, err = io.Copy(w, &io.LimitedReader{
			N: size,
			R: rand.Reader,
		})

		if err != nil {
			t.Fatalf("io.Copy(): %v\n", err)
		}
	}

	mpw.Close()

	return &buf
}

func TestUnmarshal(t *testing.T) {
	type want struct {
		File  *File
		Files []*File
	}

	tests := []struct {
		body io.Reader
		ct   string
		opts []Option
		err  error
		want struct {
			File  *File
			Files []*File
		}
	}{
		{
			strings.NewReader(""),
			"application/x-www-form-urlencoded",
			nil,
			Errors{
				"string":  {ErrFieldRequired},
				"pattern": {ErrFieldRequired},
				"int":     {ErrFieldRequired},
				"bool":    {ErrFieldRequired},
			},
			want{},
		},
		{
			strings.NewReader("unknown_field=value"),
			"application/x-www-form-urlencoded",
			nil,
			Errors{
				"string":  {ErrFieldRequired},
				"pattern": {ErrFieldRequired},
				"int":     {ErrFieldRequired},
				"bool":    {ErrFieldRequired},
				"unknown_field": {
					schema.UnknownKeyError{Key: "unknown_field"},
				},
			},
			want{},
		},
		{
			strings.NewReader("unknown_field=value"),
			"application/x-www-form-urlencoded",
			[]Option{
				IgnoreUnknownKeys(),
			},
			Errors{
				"string":  {ErrFieldRequired},
				"pattern": {ErrFieldRequired},
				"int":     {ErrFieldRequired},
				"bool":    {ErrFieldRequired},
			},
			want{},
		},
		{
			strings.NewReader(url.Values{
				"string":  {"string value"},
				"pattern": {"pattern"},
				"int":     {"10"},
				"bool":    {"false"},
			}.Encode()),
			"application/x-www-form-urlencoded",
			nil,
			nil,
			want{},
		},
		{
			strings.NewReader(""),
			"application/json",
			nil,
			nil,
			want{},
		},
		{
			strings.NewReader(`{"string": "string value", "pattern": "pattern", "int": 10, "bool": false}`),
			"application/json",
			nil,
			nil,
			want{},
		},
		{
			multipartBody(t, "file", 1, 32<<10, url.Values{
				"string":  {"string value"},
				"pattern": {"pattern"},
				"int":     {"10"},
				"bool":    {"false"},
			}),
			fmt.Sprintf("multipart/form-data; boundary=%s", multipartContentBoundary),
			nil,
			nil,
			want{
				File: &File{
					hdr: &multipart.FileHeader{
						Filename: "file.0",
						Size:     32 << 10,
					},
					typ:     "application/octet-stream",
					modTime: time.Now().Truncate(time.Second).UTC(),
				},
			},
		},
		{
			multipartBody(t, "files", 5, 32<<10, url.Values{
				"string":  {"string value"},
				"pattern": {"pattern"},
				"int":     {"10"},
				"bool":    {"false"},
			}),
			fmt.Sprintf("multipart/form-data; boundary=%s", multipartContentBoundary),
			nil,
			nil,
			want{
				Files: []*File{
					{
						hdr: &multipart.FileHeader{
							Filename: "file.0",
							Size:     32 << 10,
						},
						typ:     "application/octet-stream",
						modTime: time.Now().Truncate(time.Second).UTC(),
					},
					{
						hdr: &multipart.FileHeader{
							Filename: "file.1",
							Size:     32 << 10,
						},
						typ:     "application/octet-stream",
						modTime: time.Now().Truncate(time.Second).UTC(),
					},
					{
						hdr: &multipart.FileHeader{
							Filename: "file.2",
							Size:     32 << 10,
						},
						typ:     "application/octet-stream",
						modTime: time.Now().Truncate(time.Second).UTC(),
					},
					{
						hdr: &multipart.FileHeader{
							Filename: "file.3",
							Size:     32 << 10,
						},
						typ:     "application/octet-stream",
						modTime: time.Now().Truncate(time.Second).UTC(),
					},
					{
						hdr: &multipart.FileHeader{
							Filename: "file.4",
							Size:     32 << 10,
						},
						typ:     "application/octet-stream",
						modTime: time.Now().Truncate(time.Second).UTC(),
					},
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test.%d", i), func(t *testing.T) {
			t.Parallel()

			req := http.Request{
				Method: "POST",
				Header: http.Header{
					"Content-Type": {test.ct},
				},
				Body: io.NopCloser(test.body),
			}

			var f F

			err := Unmarshal(&req, &f, test.opts...)

			if !errors.Is(err, test.err) {
				t.Fatalf("err = %v, want = %v\n", err, test.err)
			}

			if test.want.File != nil {
				want, err := test.want.File.MarshalJSON()

				if err != nil {
					t.Fatalf("test.want.File.MarshalJSON(): %v\n", err)
				}

				got, err := f.File.MarshalJSON()

				if err != nil {
					t.Fatalf("f.File.MarshalJSON(): %v\n", err)
				}

				if string(got) != string(want) {
					t.Fatalf("f.File.MarshalJSON() = %v, want = %v\n", string(got), string(want))
				}
			}

			if l := len(test.want.Files); l > 0 {
				if l != len(f.Files) {
					t.Fatalf("len(f.Files) = %v, want = %v\n", len(f.Files), l)
				}

				for i, file := range test.want.Files {
					want, err := file.MarshalJSON()

					if err != nil {
						t.Fatalf("file.MarshalJSON(): %v\n", err)
					}

					got, err := f.Files[i].MarshalJSON()

					if err != nil {
						t.Fatalf("f.Files[%d].MarshalJSON(): %v\n", i, err)
					}

					if string(got) != string(want) {
						t.Fatalf("f.Files[%d].MarshalJSON() = %v, want = %v\n", i, string(got), string(want))
					}
				}
			}
		})
	}
}

func TestUnmarshalAndValidate(t *testing.T) {
	tests := []struct {
		body io.Reader
		ct   string
		opts []Option
		want error
	}{
		{
			strings.NewReader(""),
			"application/x-www-form-urlencoded",
			nil,
			Errors{
				"string":  {ErrFieldRequired, LengthError{Min: 6, Max: 60}},
				"pattern": {ErrFieldRequired, MatchError{Regexp: rePattern}},
				"int":     {ErrFieldRequired},
				"bool":    {ErrFieldRequired},
			},
		},
		{
			strings.NewReader("unknown_field=value"),
			"application/x-www-form-urlencoded",
			nil,
			Errors{
				"string":  {ErrFieldRequired, LengthError{Min: 6, Max: 60}},
				"pattern": {ErrFieldRequired, MatchError{Regexp: rePattern}},
				"int":     {ErrFieldRequired},
				"bool":    {ErrFieldRequired},
				"unknown_field": {
					schema.UnknownKeyError{Key: "unknown_field"},
				},
			},
		},
		{
			strings.NewReader("unknown_field=value"),
			"application/x-www-form-urlencoded",
			[]Option{
				IgnoreUnknownKeys(),
			},
			Errors{
				"string":  {ErrFieldRequired, LengthError{Min: 6, Max: 60}},
				"pattern": {ErrFieldRequired, MatchError{Regexp: rePattern}},
				"int":     {ErrFieldRequired},
				"bool":    {ErrFieldRequired},
			},
		},
		{
			strings.NewReader(""),
			"application/json",
			nil,
			Errors{
				"string":  {ErrFieldRequired, LengthError{Min: 6, Max: 60}},
				"pattern": {ErrFieldRequired, MatchError{Regexp: rePattern}},
				"bool":    {ErrFieldRequired},
			},
		},
		{
			strings.NewReader(`{"string": 10}`),
			"application/json",
			nil,
			Errors{
				"string": {
					ErrFieldRequired,
					LengthError{Min: 6, Max: 60},
					&json.UnmarshalTypeError{
						Value:  "number",
						Type:   reflect.TypeOf("string"),
						Struct: "F",
						Field:  "string",
					},
				},
				"pattern": {ErrFieldRequired, MatchError{Regexp: rePattern}},
				"bool":    {ErrFieldRequired},
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test.%d", i), func(t *testing.T) {
			t.Parallel()

			req := http.Request{
				Method: "POST",
				Header: http.Header{
					"Content-Type": {test.ct},
				},
				Body: io.NopCloser(test.body),
			}

			var f F

			err := UnmarshalAndValidate(&req, &f, test.opts...)

			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want = %v\n", err, test.want)
			}
		})
	}
}

type NilForm struct{}

func (f NilForm) Fields() Fields                 { return nil }
func (f NilForm) Validate(context.Context) error { return nil }

func TestFormFields(t *testing.T) {
	tests := []struct {
		form  Form
		field string
		want  string
	}{
		{
			NilForm{},
			"string",
			"",
		},
		{
			&F{},
			"secret",
			"",
		},
		{
			&F{Secret: "secret"},
			"secret",
			"",
		},
		{
			&F{String: "string"},
			"string",
			"string",
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test.%d", i), func(t *testing.T) {
			t.Parallel()

			fields := test.form.Fields()

			got := fields.Get(test.field)

			if got != test.want {
				t.Errorf("fields.Get(%q) = %v, want = %v\n", test.field, got, test.want)
			}
		})
	}
}

func TestFlash_Old(t *testing.T) {
	pair := [][]byte{
		make([]byte, 32),
		make([]byte, 32),
	}

	rand.Read(pair[0])
	rand.Read(pair[1])

	store := sessions.NewFilesystemStore("", pair...)
	store.MaxAge(8640)

	t.Cleanup(func() {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "session_*"))

		for _, match := range matches {
			os.Remove(match)
		}
	})

	type want struct {
		form F
		errs Errors
	}

	tests := []struct {
		form Form
		errs Errors
		want want
	}{
		{
			nil,
			nil,
			want{F{}, nil},
		},
		{
			&F{
				String:  "value",
				Pattern: "foo",
				Int:     10,
			},
			nil,
			want{
				F{
					String:  "value",
					Pattern: "foo",
					Int:     10,
				},
				nil,
			},
		},
		{
			nil,
			Errors{
				"string":  {ErrFieldRequired},
				"pattern": {ErrFieldRequired, MatchError{Regexp: reEmail}},
			},
			want{
				F{},
				Errors{
					"string":  {errors.New("field is required")},
					"pattern": {errors.New("field is required"), fmt.Errorf("does not match pattern %q", "@")},
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test.%d", i), func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			resp := httptest.NewRecorder()

			name := strings.Replace(t.Name(), "/", "-", -1)

			sess, err := store.Get(req, name)

			if err != nil {
				t.Fatalf("store.Get(req, %q): %v\n", name, err)
			}

			Flash(sess, test.form, test.errs)

			if err := sess.Save(req, resp); err != nil {
				t.Fatalf("sess.Save(%v, resp): %v\n", req, err)
			}

			sess, err = store.Get(req, name)

			if err != nil {
				t.Fatalf("store.Get(req, %q): %v\n", t.Name(), err)
			}

			var (
				form F
				errs Errors = make(Errors)
			)

			Old(sess, &form, errs)

			if form.String != test.want.form.String {
				t.Errorf("form.String = %v, want = %v\n", form.String, test.want.form.String)
			}

			if form.Pattern != test.want.form.Pattern {
				t.Errorf("form.Pattern = %v, want = %v\n", form.Pattern, test.want.form.Pattern)
			}

			if form.Int != test.want.form.Int {
				t.Errorf("form.Int = %v, want = %v\n", form.Int, test.want.form.Int)
			}

			if form.Secret != test.want.form.Secret {
				t.Errorf("form.Secret = %v, want = %v\n", form.Secret, test.want.form.Secret)
			}

			if !errs.Is(test.want.errs) {
				t.Errorf("errs = %#v, want = %#v\n", errs, test.want.errs)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	type want struct {
		str   string
		count int
	}

	tests := []struct {
		errs  Errors
		field string
		want  want
	}{
		{
			Errors{},
			"",
			want{},
		},
		{
			Errors{
				"email": {ErrFieldRequired},
			},
			"email",
			want{"email field is required", 1},
		},
		{
			Errors{
				"email": {
					ErrFieldRequired,
					MatchError{Regexp: reEmail},
				},
			},
			"email",
			want{"email field is required (and 1 other error)", 2},
		},
		{
			Errors{
				"email": {
					ErrFieldRequired,
					MatchError{Regexp: reEmail},
				},
				"username": {ErrFieldRequired},
			},
			"email",
			want{"email field is required (and 2 other errors)", 2},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test.%d", i), func(t *testing.T) {
			t.Parallel()

			errs := test.errs.Get(test.field)

			if l := len(errs); l != test.want.count {
				t.Errorf("len(errs) = %v, want = %v\n", l, test.want.count)
			}

			if s := test.errs.Error(); s != test.want.str {
				t.Errorf("test.errs.Error() = %v, want = %v\n", s, test.want.str)
			}
		})
	}
}

func TestErrorsJSON(t *testing.T) {
	errs := Errors{
		"email": {
			ErrFieldRequired,
			MatchError{Regexp: reEmail},
		},
		"username": {ErrFieldRequired},
	}

	b, err := json.Marshal(errs)

	if err != nil {
		t.Fatalf("json.Marshal(%#v): %v\n", errs, err)
	}

	want := `{"email":["field is required","does not match pattern \"@\""],"username":["field is required"]}`

	if s := string(b); s != want {
		t.Fatalf("unexpected json.Marshal() value:\n\twant = %v\n\tgot  = %v\n", s, want)
	}

	errs2 := make(Errors)

	if err := errs2.UnmarshalJSON(b); err != nil {
		t.Fatalf("errs2.UnmarshalJSON(%q): %v\n", string(b), err)
	}
}
