package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	bolt "go.etcd.io/bbolt"
)

type app struct {
	AssetVersion string
	Cfg          *Config
	DB           *bolt.DB
	NoteService  *NoteService
	RateLimiter  *RateLimiter
	StaticFS     fs.FS
	Views        *Views
}

func main() {
	var dev bool
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.BoolVar(&dev, "dev", false, "run in local development mode and load .env")
	if err := flags.Parse(os.Args[1:]); err != nil {
		logServerError("flag_parse_failed", err)
		os.Exit(1)
	}
	if flags.NArg() != 0 {
		slog.Error(logMsgStartupFailed, "event", "unexpected_positional_arguments", "count", flags.NArg())
		os.Exit(1)
	}

	getenv := os.Getenv
	if dev {
		env, err := loadDevelopmentEnv()
		if err != nil {
			logServerError("development_environment_load_failed", err)
			os.Exit(1)
		}
		getenv = env
	}

	err := run(context.Background(), getenv, StartupOptions{Development: dev})
	if err != nil {
		logServerError("application_run_failed", err)
		os.Exit(1)
	}
}

func loadDevelopmentEnv() (func(string) string, error) {
	env, err := godotenv.Read()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if _, ok := env[envEnvironment]; !ok {
		env[envEnvironment] = environmentDevelopment
	}
	return func(key string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		if value, ok := env[key]; ok {
			return value
		}
		return ""
	}, nil
}

func run(ctx context.Context, getenv func(string) string, options StartupOptions) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := NewConfigWithOptions(getenv, options)
	if err != nil {
		return fmt.Errorf("error creating config: %w", err)
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer closeAndLogError(db)

	noteService := NewNoteService(db, cfg.MaxNoteSize)
	if err := noteService.StartupMaintenance(); err != nil {
		return fmt.Errorf("error initializing database state: %w", err)
	}

	staticFS, templateFS := applicationFileSystems(cfg)
	assetVersion, err := NewAssetVersion(staticFS)
	if err != nil {
		return fmt.Errorf("error creating asset version: %w", err)
	}
	views, err := NewViews(templateFS, assetVersion, cfg.IsDevelopment, cfg.Brand)
	if err != nil {
		return fmt.Errorf("error parsing templates: %w", err)
	}

	app := &app{
		AssetVersion: assetVersion,
		Cfg:          cfg,
		DB:           db,
		NoteService:  noteService,
		RateLimiter:  NewRateLimiter(cfg.RateLimit),
		StaticFS:     staticFS,
		Views:        views,
	}

	handler := newServerHandler(app)
	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	if err := runServer(ctx, server, app); err != nil {
		return err
	}

	slog.Debug(logMsgServerLifecycle, "event", "shutdown_complete")
	return nil
}

func applicationFileSystems(cfg *Config) (fs.FS, fs.FS) {
	if cfg.IsDevelopment {
		return os.DirFS("web/static"), os.DirFS("web/html")
	}
	return embeddedStaticFS(), embeddedTemplateFS()
}

func newServerHandler(app *app) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", HandleCreateNoteView(app.Views, app.Cfg))
	mux.Handle("GET /note/{id}", HandlePreviewNote(app.Views))
	mux.Handle("GET /api/config", HandleGetConfig(app.Cfg))
	mux.Handle("POST /api/notes/{id}", HandleCreateNote(app.NoteService))
	mux.Handle("POST /api/notes/{id}/open", HandleOpenNote(app.NoteService))
	mux.Handle("POST /api/tickets", HandleCreateTicket(app.NoteService))
	mux.HandleFunc("GET /static/{asset}", http.NotFound)
	mux.Handle("GET /static/{version}/{asset...}", HandleStaticAssets(app.AssetVersion, app.StaticFS))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		setNoStore(w)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/", app.Views.NotFound)

	var handler http.Handler = mux
	handler = CSRFMiddleware(handler)
	handler = RateLimitMiddleware(app.RateLimiter, handler)
	handler = SecurityHeadersMiddleware(app.Cfg, handler)
	handler = LoggingMiddleware(handler)
	handler = TrustedProxyMiddleware(app.Cfg, handler)
	handler = HTTPContextMiddleware(handler)
	return handler
}

func runServer(ctx context.Context, server *http.Server, app *app) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("error listening on %s: %w", server.Addr, err)
	}
	return serveServer(ctx, server, app, listener)
}

func serveServer(ctx context.Context, server *http.Server, app *app, listener net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	serveErr := make(chan error, 1)
	wg.Go(func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logServerError("http_server_failed", err, "addr", server.Addr)
			serveErr <- err
		} else {
			serveErr <- nil
		}
		cancel()
	})
	wg.Go(func() {
		ticker := time.NewTicker(app.Cfg.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := app.NoteService.Cleanup(); err != nil {
					logServerError("background_cleanup_failed", err)
				}
			}
		}
	})

	logStartupSummary(app.Cfg, listener.Addr().String())
	slog.Info(logMsgServerLifecycle, "event", "server_listening", "addr", listener.Addr().String())
	<-ctx.Done()
	slog.Debug(logMsgServerLifecycle, "event", "shutdown_started")

	shutdownCtx := context.Background()
	if app.Cfg.GracePeriod > 0 {
		var cancelTimeout context.CancelFunc
		shutdownCtx, cancelTimeout = context.WithTimeout(context.Background(), app.Cfg.GracePeriod)
		defer cancelTimeout()
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		logServerError("http_server_shutdown_failed", err, "addr", server.Addr)
		if err = server.Close(); err != nil {
			logServerError("http_server_close_failed", err, "addr", server.Addr)
		}
	}

	wg.Wait()
	if err := <-serveErr; err != nil {
		return fmt.Errorf("error serving HTTP: %w", err)
	}
	return nil
}
