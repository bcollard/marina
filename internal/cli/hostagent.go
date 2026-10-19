package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/lima-vm/lima/v2/pkg/hostagent"
	"github.com/lima-vm/lima/v2/pkg/hostagent/api/server"
	"github.com/lima-vm/lima/v2/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newHostagentCmd returns the hidden hostagent subcommand that Lima spawns as a
// detached subprocess when starting a VM instance.  The command signature and
// flags must stay in sync with what Lima's instance.StartWithPaths() passes.
func newHostagentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hostagent INSTANCE",
		Short:  "Run the Lima host agent (spawned internally by marina)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE:   runHostagent,
	}
	cmd.Flags().StringP("pidfile", "p", "", "Write PID to file")
	cmd.Flags().String("socket", "", "Path of the hostagent Unix socket")
	cmd.Flags().Bool("run-gui", false, "Run the VZ GUI synchronously within hostagent")
	cmd.Flags().String("guestagent", "", "Local file path of lima-guestagent.OS-ARCH[.gz]")
	cmd.Flags().String("nerdctl-archive", "", "Local file path of nerdctl-full archive")
	cmd.Flags().Bool("progress", false, "Show provision script progress via cloud-init logs")
	return cmd
}

func runHostagent(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	pidfile, _ := cmd.Flags().GetString("pidfile")
	if pidfile != "" {
		if existing, err := store.ReadPIDFile(pidfile); existing != 0 {
			return fmt.Errorf("another hostagent may already be running with pid %d (pidfile %q)", existing, pidfile)
		} else if err != nil {
			return fmt.Errorf("checking existing hostagent pidfile: %w", err)
		}
		// WritePIDFile records the boot session, so a pidfile left by a previous
		// boot is discarded instead of matching a reused PID.
		if err := store.WritePIDFile(pidfile, os.Getpid()); err != nil {
			return err
		}
		defer os.RemoveAll(pidfile)
	}

	socket, _ := cmd.Flags().GetString("socket")
	if socket == "" {
		return errors.New("--socket must be specified")
	}

	instName := args[0]

	runGUI, _ := cmd.Flags().GetBool("run-gui")
	if runGUI {
		// Required for vz.RunGUI; must be called before any VZ CGO loads.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	stdout := &syncWriter{w: cmd.OutOrStdout()}
	stderr := &syncWriter{w: cmd.ErrOrStderr()}
	defer stdout.flush()
	defer stderr.flush()
	initHostagentLogrus(stderr)

	var opts []hostagent.Opt
	if ga, _ := cmd.Flags().GetString("guestagent"); ga != "" {
		opts = append(opts, hostagent.WithGuestAgentBinary(ga))
	}
	if na, _ := cmd.Flags().GetString("nerdctl-archive"); na != "" {
		opts = append(opts, hostagent.WithNerdctlArchive(na))
	}
	if prog, _ := cmd.Flags().GetBool("progress"); prog {
		opts = append(opts, hostagent.WithCloudInitProgress(prog))
	}

	ha, err := hostagent.New(ctx, instName, stdout, signalCh, opts...)
	if err != nil {
		return err
	}

	backend := &server.Backend{Agent: ha}
	mux := http.NewServeMux()
	server.AddRoutes(mux, backend)
	srv := &http.Server{Handler: mux}

	if err := os.RemoveAll(socket); err != nil {
		return err
	}
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "unix", socket)
	if err != nil {
		return err
	}
	logrus.Infof("hostagent socket created at %s", socket)
	go func() {
		if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Warn("hostagent API server exited with error")
		}
	}()
	defer srv.Close()

	return ha.Run(ctx)
}

// initHostagentLogrus configures logrus with JSON format on stderr.
// Lima's hostagent event watcher (pkg/hostagent/events.Watcher) parses
// JSON log lines to detect VM readiness — the format must not change.
func initHostagentLogrus(stderr io.Writer) {
	logrus.SetOutput(stderr)
	logrus.SetFormatter(new(logrus.JSONFormatter))
	if logrus.GetLevel() == logrus.DebugLevel {
		logrus.SetLevel(logrus.TraceLevel)
	} else {
		logrus.SetLevel(logrus.DebugLevel)
	}
}

// syncInterval is how long syncWriter batches writes before flushing them.
// Ported from Lima v2.2.1: an fsync per write serialised the hostagent behind
// logrus's output mutex, and the port forwarder logs once per tunnel teardown,
// so a burst of closing connections throttled all logging. The flush stays,
// batched, because the event watcher follows these files.
const syncInterval = 100 * time.Millisecond

// syncWriter wraps an io.Writer and flushes it at most once per syncInterval.
type syncWriter struct {
	w io.Writer

	mu    sync.Mutex
	dirty bool
	timer *time.Timer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.w.Write(p)
	if err == nil {
		w.dirty = true
		if w.timer == nil {
			w.timer = time.AfterFunc(syncInterval, w.flush)
		}
	}
	return n, err
}

// flush stops the pending timer and syncs any writes in the current batch.
func (w *syncWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	if !w.dirty {
		return
	}
	if s, ok := w.w.(interface{ Sync() error }); ok {
		_ = s.Sync()
	}
	w.dirty = false
}
