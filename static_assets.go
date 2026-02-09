package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const immutableStaticCacheControl = "public, max-age=31536000, immutable"

//go:embed web/static/*
var embeddedStaticFiles embed.FS

//go:embed web/html/*.gohtml
var embeddedTemplateFiles embed.FS

func embeddedStaticFS() fs.FS {
	staticFS, err := fs.Sub(embeddedStaticFiles, "web/static")
	if err != nil {
		panic(err)
	}
	return staticFS
}

func embeddedTemplateFS() fs.FS {
	templateFS, err := fs.Sub(embeddedTemplateFiles, "web/html")
	if err != nil {
		panic(err)
	}
	return templateFS
}

func NewAssetVersion(staticFS fs.FS) (string, error) {
	hash := sha256.New()

	err := fs.WalkDir(staticFS, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("error reading static asset metadata %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		data, err := fs.ReadFile(staticFS, name)
		if err != nil {
			return fmt.Errorf("error reading static asset %q: %w", name, err)
		}

		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error creating asset version: %w", err)
	}

	sum := hash.Sum(nil)
	return hex.EncodeToString(sum)[:16], nil
}

func HandleStaticAssets(version string, staticFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(staticFS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("version") != version {
			http.NotFound(w, r)
			return
		}

		assetName := r.PathValue("asset")
		if assetName == "" ||
			assetName == "." ||
			assetName == ".." ||
			strings.HasPrefix(assetName, "../") ||
			strings.Contains(assetName, "/../") {
			http.NotFound(w, r)
			return
		}
		asset := "/" + path.Clean(assetName)

		r2 := new(http.Request)
		*r2 = *r
		u2 := *r.URL
		u2.Path = asset
		u2.RawPath = ""
		r2.URL = &u2

		w.Header().Set("Cache-Control", immutableStaticCacheControl)
		fileServer.ServeHTTP(w, r2)
	})
}
