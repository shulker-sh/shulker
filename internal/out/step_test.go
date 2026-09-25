package out

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestStepsSettleOffTerminal(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := &Printer{Stdout: &stdout, Stderr: &stderr}
	p.Step("fetching Minecraft %s", "26.2")
	p.Step("installing neoforge 26.2.0.87")
	p.Step("fetching Minecraft %s", "26.2")
	p.Out().Text("result")
	want := "  ✔ fetched Minecraft 26.2\n  ✔ installed neoforge 26.2.0.87\n"
	if stderr.String() != want || stdout.String() != "  result\n" {
		t.Fatalf("stderr %q stdout %q", stderr.String(), stdout.String())
	}
}

func TestDownloadBarSettlesTheRunningStep(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.Step("fetching fabric loader 0.19.5 for 26.2")
	pr := p.Progress("fetching", []Download{{Name: "a.jar", Size: 1}})
	if stderr.String() != "  ✔ fetched fabric loader 0.19.5 for 26.2\n" {
		t.Fatalf("the step must settle before the bar draws: %q", stderr.String())
	}
	pr.Advance()
	pr.Finish()
}

func TestStepWording(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.Step("keeping sodium 0.9 already in lock")
	p.Step("verifying build provenance with gh")
	p.Step("checksum verified")
	p.Settle()
	want := "  ✔ kept sodium 0.9 already in lock\n  ✔ verified build provenance with gh\n  ✔ checksum verified\n"
	if stderr.String() != want {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestFailedStepIsNotMarkedDone(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.Step("fetching a")
	p.Step("cloning b")
	p.Fail(errors.New("boom"))
	if got := stderr.String(); !strings.HasPrefix(got, "  ✔ fetched a\n") || strings.Contains(got, "cloned b") {
		t.Fatalf("stderr: %q", got)
	}
}

func TestDroppedStepLeavesNoLine(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.Step("fetching a")
	p.Drop()
	p.Warn("offline, using a")
	if got := stderr.String(); strings.Contains(got, "fetched a") || !strings.Contains(got, "offline, using a") {
		t.Fatalf("stderr: %q", got)
	}
}

func TestStepsStayQuietInJSON(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{JSON: true, Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.Step("fetching a")
	p.Settle()
	if stderr.Len() != 0 {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestSpinnerFramesSitOneSpaceBeforeTheText(t *testing.T) {
	for _, theme := range []Theme{{}, {ASCII: true}} {
		s := newSpinner(theme)
		for range s.Spinner.Frames {
			if frame := s.View(); frame != strings.TrimSpace(frame) || Width(frame) != 1 {
				t.Fatalf("ascii %v: frame %q", theme.ASCII, frame)
			}
			s, _ = s.Update(spinner.TickMsg{})
		}
	}
	if got := newSpinner(Theme{ASCII: true}).View(); got != "|" {
		t.Fatalf("ascii spinner starts at %q", got)
	}
	if got := newSpinner(Theme{HasColor: true}).View(); got != "\x1b[1;36m⣾\x1b[0m" && got != "\x1b[36;1m⣾\x1b[0m" {
		t.Fatalf("coloured spinner %q", got)
	}
}
