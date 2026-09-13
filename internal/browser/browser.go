// Package browser renders JavaScript-heavy pages with a locally installed
// Chrome/Chromium-family browser via the DevTools protocol. It is optional:
// everything else in tokensaver works without it.
package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

// Timeout bounds one render, including browser start-up.
const Timeout = 45 * time.Second

var (
	findOnce sync.Once
	execPath string
)

// ExecPath returns the browser binary to use ("" if none is installed). The
// TOKENSAVER_CHROME environment variable overrides the search.
func ExecPath() string {
	findOnce.Do(func() { execPath = find() })
	return execPath
}

func find() string {
	if p := os.Getenv("TOKENSAVER_CHROME"); p != "" {
		return p
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		for _, app := range []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Chromium.app/Contents/MacOS/Chromium",
			"Brave Browser.app/Contents/MacOS/Brave Browser",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		} {
			candidates = append(candidates, filepath.Join("/Applications", app))
			if home, err := os.UserHomeDir(); err == nil {
				candidates = append(candidates, filepath.Join(home, "Applications", app))
			}
		}
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if dir := os.Getenv(env); dir != "" {
				candidates = append(candidates,
					filepath.Join(dir, `Google\Chrome\Application\chrome.exe`),
					filepath.Join(dir, `Microsoft\Edge\Application\msedge.exe`),
					filepath.Join(dir, `BraveSoftware\Brave-Browser\Application\brave.exe`))
			}
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "headless_shell", "brave-browser", "microsoft-edge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// ErrNoBrowser means no Chrome/Chromium-family browser was found.
var ErrNoBrowser = errors.New("no Chrome/Chromium browser found (install one or set TOKENSAVER_CHROME to its path)")

// Render loads url in a fresh headless browser with a throwaway profile, waits
// for the page's text to stop changing, and returns the resulting HTML.
func Render(ctx context.Context, url string) (string, error) {
	path := ExecPath()
	if path == "" {
		return "", ErrNoBrowser
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(path),
		chromedp.UserAgent(source.UserAgent),
		chromedp.WindowSize(1280, 2000),
	)
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	tabCtx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	var html string
	err := chromedp.Run(tabCtx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		waitForStableText(),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	return html, err
}

// waitForStableText polls the body's text length until it has been unchanged
// for two consecutive checks (content finished rendering), up to ~10 seconds.
func waitForStableText() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		prev, stable := -1, 0
		for range 25 {
			var n int
			if err := chromedp.Evaluate(`document.body ? document.body.innerText.length : 0`, &n).Do(ctx); err != nil {
				return err
			}
			if n > 0 && n == prev {
				if stable++; stable >= 2 {
					return nil
				}
			} else {
				stable = 0
			}
			prev = n
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
		return nil
	})
}
