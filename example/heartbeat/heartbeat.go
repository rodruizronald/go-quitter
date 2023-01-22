package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/rodruizronald/go-quitter"
)

const (
	quitTimeout = time.Duration(2 * time.Second)
)

const (
	InterruptChanIdx int = iota
)

func main() {
	mainQuitter, exit := initMainQuitter()

	// If false, quitter has already quitted.
	hb := HeartBeat{name: "main_heartbeat"}
	if ok := mainQuitter.AddGoRoutine(hb.RunMain); !ok {
		exit()
	}

	exit()
}

func initMainQuitter() (*quitter.Quitter, func()) {
	signalChan := make(chan os.Signal, 1)

	// Listen for OS interrupt signals.
	signal.Notify(signalChan, os.Interrupt)

	// List of channels to listen for quit.
	quitChans := []interface{}{signalChan}

	// For logging purpose, map of quit channels with a description.
	chansMap := make(map[int]string, len(quitChans))
	chansMap[InterruptChanIdx] = "OS interrupt signal"

	// Must use main quitter in the main goroutine.
	mainQuitter, exitFunc := quitter.NewMainQuitter(quitTimeout, quitChans)

	exitMain := func() {
		exitCode, selectedChanIdx, timeouts := exitFunc()
		fmt.Printf("Received quit from channel '%s'\n", chansMap[selectedChanIdx])

		switch exitCode {
		case 0:
			fmt.Println("Sucesfully quit application, all forked goroutines returned")
		case 1:
			fmt.Println("Failed to quit application, not all forked goroutines returned")
			for _, t := range timeouts {
				fmt.Printf("Timeout waiting done on quitter '%s' due to the following goroutines:\n", t.QuitterName)
				for _, gr := range t.GoRoutines {
					fmt.Printf("\t-  %s\n", gr)
				}
				fmt.Printf("\n")
			}
		default:
			fmt.Println("Quitter exit with unknown code", exitCode)
		}

		os.Exit(exitCode)
	}

	return mainQuitter, exitMain
}

type HeartBeat struct {
	name string
}

// Main heartbeat runs forever.
func (hb *HeartBeat) RunMain(parentQuitter quitter.GoRoutineQuitter) {
	// If nil, quitter has already quitted.
	childQuitter := quitter.NewChildQuitter(parentQuitter, "childQuitter")
	if childQuitter == nil {
		return
	}

	// Fork two childs heartbeats on the child quitter.
	// If false, quitter has already quitted.
	childA := HeartBeat{name: "child_heartbeat_a"}
	if ok := childQuitter.AddGoRoutine(childA.RunChild); !ok {
		return
	}
	childB := HeartBeat{name: "child_heartbeat_b"}
	if ok := childQuitter.AddGoRoutine(childB.RunChild); !ok {
		return
	}

	aliveInSec := 1
	for !parentQuitter.HasToQuit() {
		time.Sleep(1 * time.Second)
		fmt.Printf("Heartbeat from '%s'\n", hb.name)

		// Childs should return after 5 seconds.
		if aliveInSec == 5 {
			childQuitter.SendQuit()
			if waitTimeout, _ := childQuitter.WaitDone(2 * time.Second); waitTimeout {
				fmt.Println("Quitter timeout waiting done")
			}
		}

		aliveInSec++
	}
}

// Child heartbeat runs until main send quit.
func (hb *HeartBeat) RunChild(parentQuitter quitter.GoRoutineQuitter) {
	for !parentQuitter.HasToQuit() {
		time.Sleep(1 * time.Second)
		fmt.Printf("Heartbeat from '%s'\n", hb.name)
	}
}
