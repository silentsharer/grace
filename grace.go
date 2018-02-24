// Package grace provides a easy way to graceful restart or
// shutdown http server,compatible with systemd and supervisor.
package grace

import (
	"errors"
	"net/http"
	"os"
	"sync"
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
var (
	defaultRestartSignal   = []syscall.Signal{syscall.SIGUSR1}
	defaultShutdownSignal  = []syscall.Signal{syscall.SIGTERM, syscall.SIGINT}
	defaultPulseInterval   = time.Second
	defaultShutdownTimeout = 60 * time.Second
)

// wait all goroutine from http connection
var wg sync.WaitGroup

type Server struct {
	opt      *option
	addrs    []string
	handlers []http.Handler
}

func NewServer(opt ...Option) *Server {
	option := &option{
		restartSignal:   defaultRestartSignal,
		shutdownSignal:  defaultShutdownSignal,
		pulseInterval:   defaultPulseInterval,
		shutdownTimeout: defaultShutdownTimeout,
	}

	for _, opt := range opt {
		opt(option)
	}

	return &Server{
		addrs:    make([]string, 0),
		handlers: make([]http.Handler, 0),
		opt:      option,
	}
}

func ListenAndServe(addr string, handler http.Handler, opt ...Option) error {
	server := NewServer(opt...)
	return server.ListenAndServe(addr, handler)
}

func Go(f func()) {
	go func() {
		wg.Add(1)
		defer wg.Done()
		f()
	}()
}

// Register regist a pair of addr and router.
func (s *Server) Register(addr string, handler http.Handler) {
	s.addrs = append(s.addrs, addr)
	s.handlers = append(s.handlers, handler)
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

func (s *Server) Restart() error {
	ppid := os.Getppid()
	if IsWorker() && ppid != 1 && len(s.opt.restartSignal) > 0 {
		return syscall.Kill(ppid, s.opt.restartSignal[0])
	}

	return nil
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
