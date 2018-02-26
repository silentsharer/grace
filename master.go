package grace

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

type master struct {
	addrs      []string   // addrs to be listen
	opt        *option    // option config
	extraFiles []*os.File // listen fds transfer to worker
	workerPid  int        // worker pid
	workerExit chan error // worker exit status
	//  activeWorkerNum could be:
	//  0: all workers exit,
	//  1: worker running,
	//  2: restarting, new worker is up and old worker about to exit
	//
	//  if activeWorkerNum down to 0, we kill master as well
	activeWorkerNum int32
	sync.Mutex
}

// run master
func (m *master) run() error {
	m.Lock()

	// init fds
	err := m.initFds()
	if err != nil {
		return err
	}

	// new worker
	wpid, err := m.newWorker()
	if err != nil {
		return err
	}

	m.workerPid = wpid
	m.Unlock()

	// wait signal
	m.waitSignal()
	return nil
}

// restart graceful restart
func (m *master) restart() {
	m.Lock()
	defer m.Unlock()

	// new worker
	wpid, err := m.newWorker()
	if err != nil {
		log.Printf("[restart] fork worker error: %v\n", err)
		return
	}

	// kill old worker
	m.killWorker()

	m.workerPid = wpid
}

// shutdown master, kill worker.
func (m *master) shutdown() {
	m.killWorker()
}

// new worker
func (m *master) newWorker() (int, error) {
	return m.fork()
}

// kill worker
func (m *master) killWorker() {
	// tell old worker I have a new worker, you should go away.
	err := syscall.Kill(m.workerPid, WorkerStopSignal)
	if err != nil {
		log.Printf("[warning] kill old worker error: %v\n", err)
	}
}

// initFds clone tcp listen fds, close fds from master.
func (m *master) initFds() error {
	m.extraFiles = make([]*os.File, 0, len(m.addrs))
	for _, addr := range m.addrs {
		a, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			return fmt.Errorf("invalid address %s (%s)", addr, err)
		}
		l, err := net.ListenTCP("tcp", a)
		if err != nil {
			return err
		}
		f, err := l.File()
		if err != nil {
			return err
		}
		err = l.Close()
		if err != nil {
			return err
		}

		m.extraFiles = append(m.extraFiles, f)
	}
	return nil
}

// fork fork a worker process.
func (m *master) fork() (int, error) {
	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}

	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = m.extraFiles
	cmd.Env = append(os.Environ(), m.environ()...)

	err := cmd.Start()
	if err != nil {
		return 0, err
	}

	atomic.AddInt32(&m.activeWorkerNum, 1)
	go func() {
		m.workerExit <- cmd.Wait()
	}()

	return cmd.Process.Pid, nil
}

// waitSignal recieve restart or shutdown signal.
func (m *master) waitSignal() {
	var sig os.Signal
	ch := make(chan os.Signal)
	signal.Notify(ch, m.signal()...)

	for {
		select {
		case <-m.workerExit:
			atomic.AddInt32(&m.activeWorkerNum, -1)
			if m.activeWorkerNum <= 0 {
				log.Printf("all workers exit, master shutdown")
				m.shutdown()
				return
			}
			continue
		case sig = <-ch:
			log.Printf("master got signal: %v\n", sig)
		}

		if m.isRestartSignal(sig) {
			m.restart()
		}

		if m.isShutdownSignal(sig) {
			m.shutdown()
			return
		}
	}
}

// fork worker process transfer env.
func (m *master) environ() []string {
	wokrer := fmt.Sprintf("%s=%s", EnvWorker, ValWorker)
	fdnum := fmt.Sprintf("%s=%d", EnvFdNum, len(m.extraFiles))
	oldWorkerPid := fmt.Sprintf("%s=%d", EnvOldWorkerPid, m.workerPid)

	return []string{wokrer, fdnum, oldWorkerPid}
}

// signal list master all signal.
func (m *master) signal() []os.Signal {
	sigs := make([]os.Signal, 0, len(m.opt.restartSignal)+len(m.opt.shutdownSignal))

	for _, s := range m.opt.restartSignal {
		sigs = append(sigs, s)
	}
	for _, s := range m.opt.shutdownSignal {
		sigs = append(sigs, s)
	}

	return sigs
}

func (m *master) isRestartSignal(sig os.Signal) bool {
	for _, s := range m.opt.restartSignal {
		if s == sig {
			return true
		}
	}
	return false
}

func (m *master) isShutdownSignal(sig os.Signal) bool {
	for _, s := range m.opt.shutdownSignal {
		if s == sig {
			return true
		}
	}
	return false
}
