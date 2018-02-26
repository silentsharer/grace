// Package grace provides a easy way to graceful restart or
// shutdown http server,compatible with systemd and supervisor.
package grace

import (
	"errors"
	"net/http"
	"os"
	"syscall"
	"time"
)

type Grace interface {
	ListenAndServe(addr string, handler http.Handler, opt ...Option) error
}

// env constants
const (
	EnvWorker       = "GracefulWorker"
	EnvFdNum        = "GracefulFdNum"
	EnvOldWorkerPid = "GracefulOldWorkerPid"
	ValWorker       = "ValueWorker"
)

// default option value
var defaultOption = &option{
	restartSignal:   []syscall.Signal{syscall.SIGUSR1},
	shutdownSignal:  []syscall.Signal{syscall.SIGTERM, syscall.SIGINT},
	pulseInterval:   time.Second,
	shutdownTimeout: 60 * time.Second,
}

// http server
type Server struct {
	opt      *option
	addrs    []string
	handlers []http.Handler
}

func NewServer(opt ...Option) *Server {
	option := defaultOption

	for _, opt := range opt {
		opt(option)
	}

	return &Server{
		addrs:    make([]string, 0),
		handlers: make([]http.Handler, 0),
		opt:      option,
	}
}

// http listenAndServe
func ListenAndServe(addr string, handler http.Handler, opt ...Option) error {
	server := NewServer(opt...)
	return server.ListenAndServe(addr, handler)
}

// grace goroutine, wait all goroutine return.
func Go(f func()) {
	go func() {
		wg.Add(1)
		defer wg.Done()
		f()
	}()
}

// Run run all register server
func (s *Server) Run() error {
	if len(s.addrs) == 0 {
		return errors.New("no servers")
	}

	if IsWorker() {
		worker := &worker{handlers: s.handlers, opt: s.opt, shutdownCh: make(chan struct{})}
		return worker.run()
	}

	if IsMaster() {
		master := &master{addrs: s.addrs, opt: s.opt, workerExit: make(chan error)}
		return master.run()
	}

	return errors.New("unknown server")
}

// Register regist a pair of addr and router.
func (s *Server) Register(addr string, handler http.Handler) {
	s.addrs = append(s.addrs, addr)
	s.handlers = append(s.handlers, handler)
}

func (s *Server) ListenAndServe(addr string, handler http.Handler) error {
	s.Register(addr, handler)
	return s.Run()
}

func IsWorker() bool {
	return os.Getenv(EnvWorker) == ValWorker
}

func IsMaster() bool {
	return !IsWorker()
}

// user-defined option
type option struct {
	restartSignal   []syscall.Signal
	shutdownSignal  []syscall.Signal
	pulseInterval   time.Duration
	shutdownTimeout time.Duration
}

type Option func(o *option)

// RestartSignal set restart signal, otherwise use default value.
func RestartSignal(sigs []syscall.Signal) Option {
	return func(o *option) {
		o.restartSignal = sigs
	}
}

// ShutdownSignal set shutdown signal, otherwise use default value.
func ShutdownSignal(sigs []syscall.Signal) Option {
	return func(o *option) {
		o.shutdownSignal = sigs
	}
}

// PulseInterval set pulse interval, otherwise use default value.
func PulseInterval(pulse time.Duration) Option {
	return func(o *option) {
		o.pulseInterval = pulse
	}
}

// ShutdownTimeout set shutdown timeout, otherwise use default value.
func ShutdownTimeout(timeout time.Duration) Option {
	return func(o *option) {
		o.shutdownTimeout = timeout
	}
}
