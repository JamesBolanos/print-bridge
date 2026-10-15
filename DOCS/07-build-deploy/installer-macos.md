# Installer — macOS

## Packaging format

**`.dmg`** containing the `.app` bundle, with a symlink to `/Applications` in the same DMG window (the standard "drag to Applications" pattern macOS users expect). This is simpler to produce than a `.pkg` installer and matches user expectations for a drag-and-drop app rather than a scripted install.

`create-dmg` (a common open-source CLI tool) or a manual `hdiutil` script in the GitHub Actions workflow are both reasonable ways to produce this — either is fine; pick whichever integrates more simply into the existing Actions workflow.

## What ships in the `.app` bundle

- The Fyne-built binary.
- A standard `Info.plist` with app name, bundle identifier (e.g. `com.<yourname>.printbridge` — pick a real reverse-DNS identifier, this matters even unsigned, since it's used for app data directory conventions and any future signing), and version.
- App icon.

## Signing/notarization status for release builds

The GitHub Actions release workflow uses free ad-hoc signing:

```bash
codesign --force --deep --sign - "${APP_DIR}"
codesign --verify --deep --strict --verbose=2 "${APP_DIR}"
```

This does not require an Apple Developer Program membership. It makes the `.app` bundle structurally valid by binding `Info.plist` and sealing bundled resources before the DMG is created.

The build is still not Developer ID signed or notarized, so macOS will not fully trust it as an internet-downloaded app.

## What the user will see

Because the release is not Developer ID signed and notarized, **Gatekeeper** may block a normal double-click launch:

> "'print-bridge' cannot be opened because the developer cannot be verified" (or similar, wording varies slightly by macOS version)

On some macOS versions or quarantine paths, the warning can be harsher:

> "'print-bridge' is damaged and can't be opened. You should move it to the Trash."

Documented workaround for end users:

1. In Finder, **right-click (or Control-click) the app** → select **Open**.
2. A dialog appears with an **"Open"** button (this is a different, less alarming dialog than the one triggered by double-clicking) — click it.
3. macOS remembers this choice; subsequent launches work normally via double-click.

Alternative path (mention as a fallback in docs, since some users find right-click unfamiliar): **System Settings → Privacy & Security**, scroll to the Security section, and click **"Open Anyway"** next to the print-bridge block message, then confirm.

This should be documented plainly, same tone as the Windows doc: "print-bridge isn't notarized by Apple yet, since that requires a paid developer account this free project doesn't currently have. macOS will initially block it — right-click the app and choose Open instead of double-clicking, and confirm the dialog that appears. You'll only need to do this once."

Verify the release checksum first, then remove quarantine from the copied app if Gatekeeper shows the damaged-app dialog:

```bash
xattr -dr com.apple.quarantine /Applications/print-bridge.app
```

This is the expected free distribution workaround. Paid Developer ID signing and notarization would reduce this friction, but it is intentionally not required for this project.

## Distribution note

Since the `.dmg` will be downloaded via a browser, macOS applies a quarantine attribute (`com.apple.quarantine`) that triggers the Gatekeeper check described above — this is unavoidable without notarization and is the direct cause of the workaround being necessary. No action needed here beyond documenting it; just don't be surprised if local testing (e.g. building and running directly on your own dev machine) doesn't show the same prompt, since quarantine is applied by the browser/download process, not the build itself.

## Out of scope for the current beta

- Mac App Store distribution — a separate, more involved process (sandboxing requirements, review process) not aligned with a free/independent showcase tool.
