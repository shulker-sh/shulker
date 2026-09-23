package game

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleDump = `2026-09-23 00:02:02
Full thread dump OpenJDK 64-Bit Server VM (21.0.7+6-LTS mixed mode):

Threads class SMR info:
_java_thread_list=0x0000600002a4c000, length=2, elements={
0x000000013e80b000, 0x000000013e80c000
}

"main" #1 [259] prio=5 os_prio=31 cpu=900.00ms elapsed=75.00s tid=0x000000013e80b000 nid=259 runnable  [0x000000016b8f6000]
   java.lang.Thread.State: RUNNABLE
	at toni.missingmodschecker.MissingModsWindow.open(MissingModsWindow.java:40)
	at net.minecraft.client.main.Main.main(Main.java:220)

"Render thread" #30 [43267] prio=5 os_prio=31 cpu=10.00ms elapsed=74.00s tid=0x000000013e80c000 nid=43267 waiting on condition  [0x0000000170a0e000]
   java.lang.Thread.State: TIMED_WAITING (sleeping)
	at java.lang.Thread.sleep0(java.base@21.0.7/Native Method)

"GC Thread#0" os_prio=31 cpu=1.00ms elapsed=75.00s tid=0x000000013e70a000 nid=12291 runnable

JNI global refs: 23, weak refs: 0

Heap
 garbage-first heap   total 262144K, used 10240K
`

func TestThreadsReadsEachThreadsStackFromADump(t *testing.T) {
	threads := Threads(sampleDump)
	var names []string
	for _, th := range threads {
		names = append(names, th.Name)
	}
	if got, want := strings.Join(names, ","), "main,Render thread,GC Thread#0"; got != want {
		t.Fatalf("threads = %s, want %s", got, want)
	}
	main := threads[0].Stack
	if !strings.HasPrefix(main, `"main" #1`) || !strings.HasSuffix(main, "at net.minecraft.client.main.Main.main(Main.java:220)") {
		t.Fatalf("main's stack is its header through its last frame, got:\n%s", main)
	}
}

func TestAwaitDumpReturnsTheDumpWrittenAfterTheOffset(t *testing.T) {
	log := filepath.Join(t.TempDir(), "run.log")
	before := "[Render thread/INFO]: an earlier line\nJNI global refs: 1, weak refs: 0\n"
	if err := os.WriteFile(log, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
		defer f.Close()
		half := len(sampleDump) / 2
		f.WriteString(sampleDump[:half])
		time.Sleep(50 * time.Millisecond)
		f.WriteString(sampleDump[half:])
	}()
	got, err := awaitDump(log, int64(len(before)), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "Full thread dump") || !strings.HasSuffix(got, "JNI global refs: 23, weak refs: 0") {
		t.Fatalf("dump runs from its heading to its end, got:\n%s", got)
	}
}

func TestAwaitDumpGivesUpWhenNoDumpEnds(t *testing.T) {
	log := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(log, []byte("Full thread dump OpenJDK\n\n\"main\" #1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := awaitDump(log, 0, 200*time.Millisecond); !errors.Is(err, ErrDumpTimeout) {
		t.Fatalf("err = %v, want ErrDumpTimeout", err)
	}
}
