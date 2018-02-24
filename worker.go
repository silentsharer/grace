package grace

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	WorkerStopSignal = syscall.SIGTERM
)

type worker struct {
	handlers   []http.Handler
	servers    []server
	opt        *option
	shutdownCh chan struct{}
	sync.Mutex
}

type server struct {
	listener net.Listener
	*http.Server
}

func (w *worker) run() error {
	// init servers with fds from master
	err := w.initServers()
	if err != nil {
		return err
	}

	// start http servers
	err = w.startServers()
	if err != nil {
		return err
	}

	// kill old worker
	w.killOldWorker()

	// water master
	go w.watchMaster()

	// waitSignal
	w.waitSignal()
	return nil
}

func (w *worker) initServers() error {
	fdnum, err := strconv.Atoi(os.Getenv(EnvFdNum))
	if err != nil {
		return fmt.Errorf("invalid %s integer", EnvFdNum)
	}

	if len(w.handlers) != fdnum {
		return fmt.Errorf("handler number does not match numFDs, %v!=%v", len(w.handlers), fdnum)
	}

	for i := 0; i < fdnum; i++ {
		f := os.NewFile(uintptr(3+i), "")
		l, err := net.FileListener(f)
		if err != nil {
			return fmt.Errorf("failed to inherit file descriptor: %d", i)
		}
		server := server{
			Server: &http.Server{
				Handler: w.handlers[i],
			},
			listener: l,
		}
		w.servers = append(w.servers, server)
	}
	return nil
}

func (w *worker) startServers() error {
	if len(w.servers) == 0 {
		return errors.New("no server")
	}

	for i := 0; i < len(w.servers); i++ {
		s := w.servers[i]
		go func() {
			if err := s.Serve(s.listener); err != nil {
				log.Printf("http Serve error: %v\n", err)
			}
		}()
	}

	return nil
}

// watchMaster to monitor if master dead
func (w *worker) watchMaster() error {
	for {
		// if parent id change to 1, it means parent is dead
		if os.Getppid() == 1 {
			log.Printf("master dead, stop worker\n")
			w.shutdown()
			break
		}
		time.Sleep(w.opt.pulseInterval)
	}
	w.shutdownCh <- struct{}{}
	return nil
}

func (w *worker) waitSignal() {
	ch := make(chan os.Signal)
	signal.Notify(ch, WorkerStopSignal)

	select {
	case sig := <-ch:
		log.Printf("worker got signal: %v\n", sig)
	case <-w.shutdownCh:
		log.Printf("stop worker")
	}

	w.shutdown()
}

func (w *worker) shutdown() {
	w.Lock()
	defer w.Unlock()

	for _, server := range w.servers {
		ctx, cancel := context.WithTimeout(context.Background(), w.opt.shutdownTimeout)
		defer cancel()

		err := server.Shutdown(ctx)
		if err != nil {
			log.Printf("shutdown server error: %v\n", err)
		}
	}

	wg.Wait()
}

func (w worker) killOldWorker() {
	oldWorkerPid, err := strconv.Atoi(os.Getenv(EnvOldWorkerPid))
	if err == nil && oldWorkerPid > 1 {
		// tell old worker i'm ready, you should go away
		err = syscall.Kill(oldWorkerPid, WorkerStopSignal)
		if err != nil {
			// unexpected: kill old worker fail
			log.Printf("[warning] kill old worker error: %v\n", err)
		}
	}
}
