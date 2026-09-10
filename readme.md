# form

database is a simple library that builds on top of [gorilla/schema][] to allow
for easy unmarshalling and validation of request data.

[gorilla/schema]: https://github.com/gorilla/schema

* [Quick start](#quick-start)
* [Forms](#forms)
* [File uploads](#file-uploads)
* [Validation](#validation)
* [Session flashing](#session-flashing)

## Quick start

To use the library, import it and define a new form into which request data can
be unmarshalled. Below is a simple example that unmarshals the request data into
the `LoginForm` and validates it.

```go
type LoginForm struct {
    Username string
    Password string
}

func (f *LoginForm) Fields() form.Fields {
    return form.Fields{
        "username": f.Username,
    }
}

func (f *LoginForm) Validate(ctx context.Context) error {
    var v form.Validator

    v.Add("username", f.Username, form.Required)
    v.Add("password", f.Password, form.Required)

    return v.Validate(ctx)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
    var f LoginForm

    if err := form.UnmarshalAndValidate(r, &f); err != nil {
        // Handle error.
    }

    // Handle the login.
}
```

## Forms

Forms are represented via the [form.Form][] interface which wraps two methods,

[form.Form]: https://pkg.go.dev/github.com/andrewpillar/form#Form

* `Fields` - The fields of the form, this is used for flashing form data to the
  session.
* `Validate` - For validating the content of the form once it has been
  unmarshalled.

As long as a struct implements these methods, it can be used as a form and given
to the [Unmarshal][] and [UnmarshalAndValidate][] functions.

[form.Unmarshal]: https://pkg.go.dev/github.com/andrewpillar/form#Unmarshal
[form.UnmarshalAndValidate]: https://pkg.go.dev/github.com/andrewpillar/form#UnmarshalAndValidate

Since this library is built on top of [gorilla/schema][], it makes use of the
`schema` struct tag to define which fields can be populated with request data,

```go
type SignupForm struct {
    Email    string `schema:"email"`
    Verified bool   `schema:"-"`
}
```

## File uploads

This library automatically handles unmarshalling of files from
multipart/form-data requests. Each field in a struct that has the type of
[form.File][] will have the multipart file, or files, mapped to it, as long as
there is a corresponding field in the request data for that file.

[form.File]: https://pkg.go.dev/github.com/andrewpillar/form#File

For example, assume an application is being built to handle file uploads that
takes a single file, then the form implementation would look something like,

```go
type UploadForm struct {
    Attachment *form.File
}

func (f *UploadForm) Fields() form.Fields { return nil }

func (f *UploadForm) Validate(ctx context.Context) error {
    var v form.Validator

    v.Add("attachment", f.Attachment, form.Required)

    return v.Validate(ctx)
}
```

with the above implementation, a handler can now be defined to handle the
uploading of files,

```go
func UploadHandler(w http.ResponseWriter, r *http.Request) {
    var f UploadForm

    if err := form.UnmarshalAndValidate(r, &f); err != nil {
        // Handle error.
    }

    dst, err := os.Create(f.Attachment.Name())

    if err != nil {
        // Handle error.
    }

    defer dst.Close()

    if _, err := io.Copy(dst, f.Attachment); err != nil {
        // Handle error.
    }
}
```

Multiple file uploads can be handled by simply defining a slice.

```go
type UploadForm struct {
    Attachments []*form.File
}
```

With this in place, the new handler would look like,

```go
func StoreAttachment(f *form.File) error {
    dst, err := os.Create(f.Name())

    if err != nil {
        return err
    }

    defer dst.Close()

    _, err = io.Copy(dst, f)
    return err
}

func UploadHandler(w http.ResponseWriter, r *http.Request) {
    var f UploadForm

    if err := form.UnmarshalAndValidate(r, &f); err != nil {
        // Handle error.
    }

    for _, f := range f.Attachments {
        if err := StoreAttachment(f); err != nil {
            // Handle error.
        }
    }
}
```

The [form.File][] type implements the following interfaces,

* [fs.File][]
* [fs.FileInfo][]

[fs.File]: https://pkg.go.dev/io/fs#File
[fs.FileInfo]: https://pkg.go.dev/io/fs#FileInfo

## Validation

Validation is done via the [form.Validator][] type. This has an the [Add][]
method which takes the name of the field being validated, the value for that
field, and the [Rules][] to apply to that field value. The [Validate][] method
is then used to execute all of the rules against the given form data. If any of
the rules fail, then the [form.Errors][] type is returned. This type is a map of
errors and fields for which the error occured.

[form.Validator]: https://pkg.go.dev/github.com/andrewpillar/form#Validator
[Add]: https://pkg.go.dev/github.com/andrewpillar/form#Validator.Add
[Rule]: https://pkg.go.dev/github.com/andrewpillar/form#Rule
[form.Errors]: https://pkg.go.dev/github.com/andrewpillar/form#Errors

The [form.Validator][] is used in the [Validate][] method of form
implementations to validate their inputs.

[Validate]: https://pkg.go.dev/github.com/andrewpillar/form#Form.Validate

```go
type SignupForm struct {
    Email          string
    Username       string
    Password       string
    VerifyPassword string `schema:"verify_password"`
}

func (f *SignupForm) Validate(ctx context.Context) error {
    var v form.Validator

    v.Add("email", f.Email, form.Required, form.Email)
    v.Add("username", f.Username, form.Required, form.Length(3, 32))
    v.Add("password", f.Password, form.Required, form.Length(6, 60))
    v.Add("verify_password", f.VerifyPassword, form.Required, form.Equals("password", f.Password))

    return v.Validate(ctx)
}
```

A [Rule][] is a function that takes a context, and the form value being
validated.

```go
type Rule func(ctx context.Context, val any) error
```

This library comes with the following rules out of the box,

* [Required][] - Make sure the field is given a value.
* [Matches][] - Make sure the field matches the given regex pattern.
* [Email][] - Make sure the field matches an email pattern.
* [Length][] - Make sure the field is between the given length.
* [Equals][] - Make sure the field is equal to another field's value.

[Required]: https://pkg.go.dev/github.com/andrewpillar/form#Required
[Matches]: https://pkg.go.dev/github.com/andrewpillar/form#Matches
[Email]: https://pkg.go.dev/github.com/andrewpillar/form#Email
[Length]: https://pkg.go.dev/github.com/andrewpillar/form#Length
[Equals]: https://pkg.go.dev/github.com/andrewpillar/form#Equals

## Session flashing

It is common to want to flash the form data, along with the errors, upon
validation errors so it can be displayed back to the user. This library makes
use of [gorilla/sessions][] to achieve easy flashing and retrieving of form
data, via the [form.Flash][] and [form.Old][] functions.

[gorilla/sessions]: https://github.com/gorilla/sessions
[form.Flash]: https://pkg.go.dev/github.com/andrewpillar/form#Flash
[form.Old]: https://pkg.go.dev/github.com/andrewpillar/form#Old

In the following example a handler is setup for a signup page. This will flash
the form and the errors to the session so that they can be retrieved to show to
the user,

```go
var (
    hashKey = []byte(os.Getenv("HASH_KEY"))
    blockKey = []byte(os.Getenv("BLOCK_KEY"))

    store = sessions.NewFilesystemStore("", hashKey, blockKey)
)

func SignupHandler(w http.ResponseWriter, r *http.Request) {
    sess, _ := store.Get(r, "session")

    if r.Method == "POST" {
        var f SignupForm

        if err := form.UnmarshalAndValidate(r, &f); err != nil {
            errs := make(form.Errors)

            if errors.As(err, &errs) {
                form.Flash(sess, &f, errs)
            }
            http.Redirect(w, r, r.Header.Get("Referer"), http.StatusSeeOther)
            return
        }
    }

    var (
        f    SignupForm
        errs form.Errors = make(form.Errors)
    )

    form.Old(sess, &f, errs)

    // Render form data and errors in a template back to the user.
}
```

> **Note:** Because [form.Errors][] contains the [error][] interface, the
> underlying concrete type will be lost when flashed to the session and
> subsequently retrieved. This means that the individual error types cannot be
> inspected once retrieved via a call to [form.Old][].

[error]: https://pkg.go.dev/builtin#error
