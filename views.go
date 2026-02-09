package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

type Views struct {
	FS           fs.FS
	AssetVersion string
	Brand        Brand
	Templates    map[string]*template.Template
	Reload       bool
}

type ErrorViewModel struct {
	Brand      Brand
	StatusCode int
	StatusText string
}

type CreateViewModel struct {
	Brand        Brand
	MaxNoteSize  int
	PublicOrigin string
}

type NoteViewModel struct {
	Brand Brand
	ID    string
}

func NewViews(fsys fs.FS, assetVersion string, reload bool, brand Brand) (*Views, error) {
	if brand.DisplayName == "" {
		brand.DisplayName = defaultDisplayName
	}
	views := &Views{FS: fsys, AssetVersion: assetVersion, Brand: brand, Reload: reload}
	if reload {
		return views, nil
	}

	templates := make(map[string]*template.Template)
	for _, name := range []string{"create", "error", "note"} {
		t, err := views.parse(name)
		if err != nil {
			return nil, err
		}
		templates[name] = t
	}
	views.Templates = templates
	return views, nil
}

func (v *Views) Error(w http.ResponseWriter, r *http.Request, err error) {
	ctx := GetHTTPContext(r)
	ctx.Err = err

	h := w.Header()
	setNoStore(w)
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)

	model := ErrorViewModel{
		Brand:      v.Brand,
		StatusCode: http.StatusInternalServerError,
		StatusText: http.StatusText(http.StatusInternalServerError),
	}

	v.View(w, r, "error", &model)
}

func (v *Views) NotFound(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	setNoStore(w)
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	model := ErrorViewModel{
		Brand:      v.Brand,
		StatusCode: http.StatusNotFound,
		StatusText: http.StatusText(http.StatusNotFound),
	}

	v.View(w, r, "error", &model)
}

func (v *Views) View(w http.ResponseWriter, r *http.Request, name string, data any) {
	if t := v.Templates[name]; t != nil {
		v.execute(w, r, t, data)
		return
	}

	t, err := v.parse(name)
	if err != nil {
		internalServerError(w, r, err)
		return
	}

	v.execute(w, r, t, data)
}

func (v *Views) parse(name string) (*template.Template, error) {
	t, err := template.New("layout.gohtml").
		Funcs(template.FuncMap{
			"assetPath": v.AssetPath,
		}).
		ParseFS(v.FS, "layout.gohtml", name+".gohtml")
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (v *Views) execute(w http.ResponseWriter, r *http.Request, t *template.Template, data any) {
	var buffer bytes.Buffer
	if err := t.Execute(&buffer, data); err != nil {
		internalServerError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setNoStore(w)

	_, err := io.Copy(w, &buffer)
	if err != nil {
		logRequestError(r, "response_write_failed", err)
		return
	}
}

func (v *Views) AssetPath(name string) string {
	name = strings.TrimLeft(name, "/")
	return "/static/" + v.AssetVersion + "/" + name
}

func internalServerError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := GetHTTPContext(r)
	ctx.Err = err

	h := w.Header()
	h.Del("Content-Encoding")
	h.Del("Content-Length")
	h.Del("ETag")

	setNoStore(w)
	h.Set("Content-Type", "text/plain; charset=utf-8")

	code := http.StatusInternalServerError
	w.WriteHeader(code)
	_, _ = fmt.Fprintln(w, http.StatusText(code))
}
