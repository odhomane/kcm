package cli

// wiring.go replaces the placeholder fallbacks in commands.go with real
// implementations. This is the seam between cli, tui, and gui packages.

import (
	"fmt"
	"os"
	"time"

	coremod "github.com/odhomane/kcm/internal/core"
	guipkg "github.com/odhomane/kcm/internal/gui"
	"github.com/odhomane/kcm/internal/ops"
	storemod "github.com/odhomane/kcm/internal/store"
	tuipkg "github.com/odhomane/kcm/internal/tui"
)

func init() {
	startServer = func(m, s, o interface{}, host string, port int, noBrowser bool) error {
		mgr2 := m.(*coremod.Manager)
		st2 := s.(*storemod.Store)
		op2 := o.(*ops.Ops)
		srv := guipkg.NewServer(mgr2, st2, op2, host, port)
		return srv.Run(noBrowser)
	}

	runTUI = func() error {
		return tuipkg.Run(mgr, st, op)
	}

	runWatch = func(o interface{}, interval, timeout time.Duration) error {
		op2 := o.(*ops.Ops)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			clearScreen()
			results := op2.BulkHealth(timeout)
			fmt.Printf("%-40s %-50s %s\n", "CONTEXT", "SERVER", "STATUS")
			for _, r := range results {
				status := "✔  OK"
				if !r.OK {
					status = "✘  " + r.Err
				}
				fmt.Printf("%-40s %-50s %s\n", r.ContextName, r.Server, status)
			}
			<-ticker.C
		}
	}

	// listBackups and restoreBackup are now called directly via core package
	// in commands.go — no wiring needed here.
}

func clearScreen() {
	fmt.Fprint(os.Stdout, "\033[H\033[2J")
}

// keep imports used.
var _ = storemod.Store{}
