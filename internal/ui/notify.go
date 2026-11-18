package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"grubby/internal/assets"
	"grubby/internal/github"
)

type NotificationOptions struct {
	Title    string
	Subtitle string
	Message  string
	URL      string
}

// SendNativeNotification triggers a silent native macOS notification banner.
func SendNativeNotification(title, subtitle, message string) {
	SendNativeNotificationWithOptions(NotificationOptions{
		Title:    title,
		Subtitle: subtitle,
		Message:  message,
	})
}

// SendPRNotification triggers a rich, silent, clickable notification for a single Pull Request.
func SendPRNotification(pr github.PullRequest) {
	title := fmt.Sprintf("🧌 Grubby • Pull Request #%d", pr.Number)

	repoDisplay := pr.DisplayRepo()
	subtitle := fmt.Sprintf("📦 %s", repoDisplay)
	if pr.HeadRefName != "" {
		subtitle = fmt.Sprintf("📦 %s  •  ⑂ %s", repoDisplay, pr.HeadRefName)
	}

	cleanTitle := strings.ReplaceAll(strings.ReplaceAll(pr.Title, "\r", " "), "\n", " ")
	cleanTitle = strings.TrimSpace(cleanTitle)

	var details []string
	if pr.IsDraft {
		details = append(details, "📝 Draft")
	}
	if pr.Author != "" {
		details = append(details, fmt.Sprintf("👤 @%s", pr.Author))
	}
	switch pr.ReviewDecision {
	case "APPROVED":
		details = append(details, "🟢 Approved")
	case "CHANGES_REQUESTED":
		details = append(details, "🔴 Changes Requested")
	case "REVIEW_REQUIRED":
		details = append(details, "🟡 Review Required")
	}

	body := cleanTitle
	if len(details) > 0 {
		body = fmt.Sprintf("%s\n%s", cleanTitle, strings.Join(details, "  •  "))
	}

	SendNativeNotificationWithOptions(NotificationOptions{
		Title:    title,
		Subtitle: subtitle,
		Message:  body,
		URL:      pr.URL,
	})
}

// SendNativeNotificationWithOptions dispatches a notification using GrubbyNotifier.app if available,
// falling back to terminal-notifier, and finally to osascript.
func SendNativeNotificationWithOptions(opts NotificationOptions) {
	go func() {
		// 1. Check for GrubbyNotifier.app (custom hobbit icon + click URL support)
		notifierBin := EnsureGrubbyNotifierApp()
		if notifierBin != "" {
			args := []string{
				"--title", opts.Title,
				"--message", opts.Message,
			}
			if opts.Subtitle != "" {
				args = append(args, "--subtitle", opts.Subtitle)
			}
			if opts.URL != "" {
				args = append(args, "--url", opts.URL)
			}
			cmd := exec.Command(notifierBin, args...)
			if err := cmd.Start(); err == nil {
				return
			}
		}

		// 2. Check for terminal-notifier
		if tnPath, err := exec.LookPath("terminal-notifier"); err == nil {
			args := []string{"-title", opts.Title, "-message", opts.Message}
			if opts.Subtitle != "" {
				args = append(args, "-subtitle", opts.Subtitle)
			}
			if opts.URL != "" {
				args = append(args, "-open", opts.URL)
			}
			for _, candidate := range []string{
				"internal/assets/grubby-icon.png",
				filepath.Join(os.Getenv("HOME"), ".config/grubby/grubby-icon.png"),
			} {
				if abs, err := filepath.Abs(candidate); err == nil {
					if _, err := os.Stat(abs); err == nil {
						args = append(args, "-appIcon", abs)
						break
					}
				}
			}
			cmd := exec.Command(tnPath, args...)
			if err := cmd.Run(); err == nil {
				return
			}
		}

		// 3. Fallback to silent AppleScript display notification
		sanitize := func(s string) string {
			s = strings.ReplaceAll(s, `\`, `\\`)
			s = strings.ReplaceAll(s, `"`, `\"`)
			s = strings.ReplaceAll(s, "\n", " ")
			s = strings.ReplaceAll(s, "\r", "")
			return s
		}

		sMsg := sanitize(opts.Message)
		sTitle := sanitize(opts.Title)
		sSub := sanitize(opts.Subtitle)

		var script string
		if sSub != "" {
			script = fmt.Sprintf(`display notification "%s" with title "%s" subtitle "%s"`, sMsg, sTitle, sSub)
		} else {
			script = fmt.Sprintf(`display notification "%s" with title "%s"`, sMsg, sTitle)
		}

		cmd := exec.Command("osascript", "-e", script)
		_ = cmd.Run()
	}()
}

// EnsureGrubbyNotifierApp verifies that GrubbyNotifier.app is built and ready in ~/.config/grubby/
func EnsureGrubbyNotifierApp() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		return ""
	}
	appDir := filepath.Join(home, ".config", "grubby", "GrubbyNotifier.app")
	binPath := filepath.Join(appDir, "Contents", "MacOS", "GrubbyNotifier")
	if info, err := os.Stat(binPath); err == nil && info.Mode()&0111 != 0 {
		return binPath
	}

	if err := buildGrubbyNotifierApp(appDir, binPath); err == nil {
		return binPath
	}
	return ""
}

func buildGrubbyNotifierApp(appDir, binPath string) error {
	swiftcPath, err := exec.LookPath("swiftc")
	if err != nil {
		return err
	}

	macosDir := filepath.Join(appDir, "Contents", "MacOS")
	resDir := filepath.Join(appDir, "Contents", "Resources")
	if err := os.MkdirAll(macosDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(resDir, 0755); err != nil {
		return err
	}

	// 1. Write Info.plist
	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key>
    <string>com.bronners.grubby.notifier</string>
    <key>CFBundleName</key>
    <string>Grubby</string>
    <key>CFBundleDisplayName</key>
    <string>Grubby</string>
    <key>CFBundleExecutable</key>
    <string>GrubbyNotifier</string>
    <key>CFBundleIconFile</key>
    <string>grubby.icns</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>`
	_ = os.WriteFile(filepath.Join(appDir, "Contents", "Info.plist"), []byte(plistContent), 0644)

	// 2. Generate grubby.icns from embedded PNG
	icnsPath := filepath.Join(resDir, "grubby.icns")
	if _, err := os.Stat(icnsPath); err != nil {
		_ = generateGrubbyICNS(icnsPath)
	}

	// 3. Compile Swift helper
	swiftSrc := `import Cocoa

let args = CommandLine.arguments
var title = "Grubby"
var subtitle = ""
var message = ""
var targetURL = ""

var i = 1
while i < args.count {
    switch args[i] {
    case "--title":
        if i + 1 < args.count { title = args[i+1]; i += 1 }
    case "--subtitle":
        if i + 1 < args.count { subtitle = args[i+1]; i += 1 }
    case "--message":
        if i + 1 < args.count { message = args[i+1]; i += 1 }
    case "--url":
        if i + 1 < args.count { targetURL = args[i+1]; i += 1 }
    default:
        break
    }
    i += 1
}

class AppDelegate: NSObject, NSApplicationDelegate, NSUserNotificationCenterDelegate {
    let title: String
    let subtitle: String
    let message: String
    let targetURL: String

    init(title: String, subtitle: String, message: String, targetURL: String) {
        self.title = title
        self.subtitle = subtitle
        self.message = message
        self.targetURL = targetURL
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let center = NSUserNotificationCenter.default
        center.delegate = self

        if let userNotif = notification.userInfo?[NSApplication.launchUserNotificationUserInfoKey] as? NSUserNotification {
            handleNotificationClick(userNotif)
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
                NSApp.terminate(nil)
            }
            return
        }

        let notif = NSUserNotification()
        notif.title = self.title
        notif.subtitle = self.subtitle
        notif.informativeText = self.message
        notif.soundName = nil
        if !self.targetURL.isEmpty {
            notif.userInfo = ["url": self.targetURL]
            notif.hasActionButton = true
            notif.actionButtonTitle = "Open PR"
        }

        center.deliver(notif)

        DispatchQueue.main.asyncAfter(deadline: .now() + 60.0) {
            NSApp.terminate(nil)
        }
    }

    func userNotificationCenter(_ center: NSUserNotificationCenter, shouldPresent notification: NSUserNotification) -> Bool {
        return true
    }

    func userNotificationCenter(_ center: NSUserNotificationCenter, didActivate notification: NSUserNotification) {
        handleNotificationClick(notification)
        NSApp.terminate(nil)
    }

    func handleNotificationClick(_ notification: NSUserNotification) {
        if let urlStr = notification.userInfo?["url"] as? String, let url = URL(string: urlStr) {
            NSWorkspace.shared.open(url)
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate(title: title, subtitle: subtitle, message: message, targetURL: targetURL)
app.delegate = delegate
app.run()
`
	tmpSwift := filepath.Join(os.TempDir(), fmt.Sprintf("grubby_notif_%d.swift", os.Getpid()))
	if err := os.WriteFile(tmpSwift, []byte(swiftSrc), 0644); err != nil {
		return err
	}
	defer os.Remove(tmpSwift)

	cmd := exec.Command(swiftcPath, "-O", tmpSwift, "-o", binPath)
	if err := cmd.Run(); err != nil {
		return err
	}

	// 4. Code-sign ad-hoc
	if csPath, err := exec.LookPath("codesign"); err == nil {
		_ = exec.Command(csPath, "-s", "-", "--force", appDir).Run()
	}

	return nil
}

func generateGrubbyICNS(outICNS string) error {
	sipsPath, err := exec.LookPath("sips")
	if err != nil {
		return err
	}
	iconutilPath, err := exec.LookPath("iconutil")
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "grubby_iconset_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	srcPNG := filepath.Join(tmpDir, "src.png")
	if len(assets.GrubbyIconPNG) > 0 {
		if err := os.WriteFile(srcPNG, assets.GrubbyIconPNG, 0644); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("embedded GrubbyIconPNG is empty")
	}

	iconsetDir := filepath.Join(tmpDir, "grubby.iconset")
	if err := os.MkdirAll(iconsetDir, 0755); err != nil {
		return err
	}

	sizes := []struct {
		name string
		size int
	}{
		{"icon_16x16.png", 16},
		{"icon_16x16@2x.png", 32},
		{"icon_32x32.png", 32},
		{"icon_32x32@2x.png", 64},
		{"icon_128x128.png", 128},
		{"icon_128x128@2x.png", 256},
		{"icon_256x256.png", 256},
		{"icon_256x256@2x.png", 512},
	}

	for _, s := range sizes {
		dest := filepath.Join(iconsetDir, s.name)
		_ = exec.Command(sipsPath, "-z", fmt.Sprintf("%d", s.size), fmt.Sprintf("%d", s.size), srcPNG, "--out", dest).Run()
	}

	return exec.Command(iconutilPath, "-c", "icns", iconsetDir, "-o", outICNS).Run()
}
