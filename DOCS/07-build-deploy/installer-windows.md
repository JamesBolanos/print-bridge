# Installer — Windows

## Packaging tool

**WiX Toolset** — selected because v1 should ship a true `.msi` installer at no certificate/tooling cost. The installer remains simple: one GUI executable, shortcuts, no service registration, no auto-start entry.

## What the installer does

- Copies the printer-bridge executable to `Program Files\printer-bridge\`.
- Creates Start Menu and Desktop shortcuts.
- **Does not** register any Startup/auto-run entry, per ADR-001.
- Sets the app's data directory (`%APPDATA%\PrinterBridge\`) on first run, not at install time — the app itself creates this on launch if absent.

## Signing status for v1

**Unsigned**, per `01-business/constraints.md` and ADR-003. The build pipeline's existing conditional signing hook (`WINDOWS_SIGN_CERT_BASE64` / `WINDOWS_SIGN_CERT_PASSWORD`) remains in the workflow but is not populated — this keeps the door open to enable signing later without a pipeline rewrite.

## What the user will see

Running an unsigned installer/executable on Windows triggers **SmartScreen**:

> "Windows protected your PC — Microsoft Defender SmartScreen prevented an unrecognized app from starting."

This is expected, not a bug. Documented workaround for end users (to be included in the repo README and any download/landing page):

1. When the SmartScreen dialog appears, click **"More info"**.
2. A **"Run anyway"** button appears — click it to proceed with installation.

This should be presented plainly and honestly in end-user-facing docs — e.g.: "printer-bridge is a free, independent tool and isn't yet code-signed. Windows will show a warning the first time you run the installer — this is expected for unsigned software, not a sign of a problem. Click 'More info' → 'Run anyway' to continue." Avoid burying this in fine print; a user who hits an unexplained security warning is likely to abandon the install rather than dig for docs.

## Out of scope for v1

- MSIX/Windows Store packaging — adds its own signing/certification requirements; not worth it for a free showcase tool at this stage.
- Silent/unattended install flags — not needed for a manually-installed single-user tool.
