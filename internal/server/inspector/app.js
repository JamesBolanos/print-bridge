"use strict";

(() => {
  const api = "/api/inspector";
  const refreshMs = 2500;
  const elements = {
    connectionDot: document.getElementById("connection-dot"),
    connectionStatus: document.getElementById("connection-status"),
    jobCount: document.getElementById("job-count"),
    clearButton: document.getElementById("clear-button"),
    jobList: document.getElementById("job-list"),
    emptyState: document.getElementById("empty-state"),
    noSelection: document.getElementById("no-selection"),
    detail: document.getElementById("job-detail"),
    selectedId: document.getElementById("selected-id"),
    selectedMeta: document.getElementById("selected-meta"),
    selectedTarget: document.getElementById("selected-target"),
    captureState: document.getElementById("capture-state"),
    previewState: document.getElementById("preview-state"),
    download: document.getElementById("download-link"),
    copyButton: document.getElementById("copy-button"),
    copyFeedback: document.getElementById("copy-feedback"),
    payloadHint: document.getElementById("payload-hint"),
    payload: document.getElementById("payload-view"),
    rawTab: document.getElementById("raw-tab"),
    visibleTab: document.getElementById("visible-tab"),
    hexTab: document.getElementById("hex-tab"),
    previewMessage: document.getElementById("preview-message"),
    previewImage: document.getElementById("preview-image"),
    labelIndex: document.getElementById("label-index"),
    renderButton: document.getElementById("render-button"),
    lastUpdated: document.getElementById("last-updated"),
  };

  let jobs = [];
  let selectedId = "";
  let selectedBytes = null;
  let selectedText = "";
  let activeView = "raw";
  let loading = false;
  let connected = false;
  let previewEnabled = false;

  function setConnection(isConnected, message) {
    connected = isConnected;
    elements.connectionDot.className = `dot ${isConnected ? "connected" : "disconnected"}`;
    elements.connectionStatus.textContent = message;
  }

  function describeTime(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
  }

  function createJobItem(job) {
    const item = document.createElement("li");
    const button = document.createElement("button");
    const title = document.createElement("strong");
    const summary = document.createElement("span");
    button.type = "button";
    button.className = `job-item${job.id === selectedId ? " selected" : ""}`;
    button.setAttribute("aria-current", job.id === selectedId ? "true" : "false");
    title.textContent = job.id;
    summary.textContent = `${job.byteCount} bytes · ${describeTime(job.receivedAt)}`;
    button.append(title, summary);
    button.addEventListener("click", () => selectJob(job.id));
    item.append(button);
    return item;
  }

  function renderJobs() {
    elements.jobList.replaceChildren(...jobs.map(createJobItem));
    elements.jobCount.textContent = `${jobs.length} of 100 retained jobs`;
    elements.emptyState.hidden = jobs.length !== 0;
    elements.clearButton.disabled = jobs.length === 0 || !connected;

    if (selectedId && !jobs.some((job) => job.id === selectedId)) {
      clearSelection("This job was cleared or evicted from session history.");
    }
    if (selectedId) {
      const selected = jobs.find((job) => job.id === selectedId);
      if (selected) renderSelectedMetadata(selected);
    }
  }

  function renderSelectedMetadata(job) {
    elements.selectedId.textContent = job.id;
    elements.selectedMeta.textContent = `${job.byteCount} bytes · ${describeTime(job.receivedAt)} · ${job.detectedType}`;
    elements.selectedTarget.textContent = job.target || "No target supplied";
    elements.captureState.textContent = job.captureState;
    elements.previewState.textContent = job.previewState;
    updateRenderButton(job);
    elements.download.href = `${api}/jobs/${encodeURIComponent(job.id)}/download`;
    updatePreview(job);
  }

  function updateRenderButton(job) {
    const index = Number(elements.labelIndex.value);
    elements.renderButton.disabled = !connected || !previewEnabled || job.detectedType !== "zpl" ||
      job.previewState === "queued" || job.previewState === "rendering" ||
      (job.previewState === "ready" && index === job.previewLabelIndex);
  }

  function updatePreview(job) {
    if (job.previewState === "ready") {
      elements.previewMessage.hidden = true;
      elements.previewImage.hidden = false;
      const source = `${api}/jobs/${encodeURIComponent(job.id)}/preview`;
      if (elements.previewImage.dataset.source !== source) {
        elements.previewImage.src = source;
        elements.previewImage.dataset.source = source;
      }
      return;
    }
    elements.previewImage.removeAttribute("src");
    delete elements.previewImage.dataset.source;
    elements.previewImage.hidden = true;
    elements.previewMessage.hidden = false;
    elements.previewMessage.textContent = job.detectedType !== "zpl"
      ? "This payload is not recognized as ZPL."
      : job.previewError
      ? `Preview ${job.previewState}: ${job.previewError}`
      : job.previewState === "disabled"
          ? "Rendering is disabled. Enable Labelary previews in PrintBridge settings to render."
          : `Preview ${job.previewState}.`;
  }

  function formatVisible(text) {
    let shown = "";
    for (const character of text) {
      const code = character.codePointAt(0);
      if (character === "\r") shown += "<CR>";
      else if (character === "\n") shown += "<LF>\n";
      else if (character === "\t") shown += "<TAB>";
      else if (character === "\0") shown += "<NUL>";
      else if (code === 0x1b) shown += "<ESC>";
      else if (code < 0x20 || (code >= 0x7f && code <= 0x9f)) {
        shown += `<U+${code.toString(16).toUpperCase().padStart(4, "0")}>`;
      } else shown += character;
    }
    return shown;
  }

  function formatHex(bytes) {
    const lines = [];
    for (let offset = 0; offset < bytes.length; offset += 16) {
      const slice = bytes.subarray(offset, Math.min(offset + 16, bytes.length));
      const hex = Array.from(slice, (byte) => byte.toString(16).padStart(2, "0")).join(" ");
      const paddedHex = hex.padEnd(47, " ");
      const ascii = Array.from(slice, (byte) => byte >= 0x20 && byte <= 0x7e ? String.fromCharCode(byte) : ".").join("");
      lines.push(`${offset.toString(16).padStart(8, "0")}  ${paddedHex}  |${ascii}|`);
    }
    return lines.join("\n") || "No payload bytes.";
  }

  function renderPayload() {
    if (selectedBytes === null) return;
    if (activeView === "visible") {
      elements.payload.textContent = formatVisible(selectedText);
      elements.payloadHint.textContent = "CR, LF, TAB, NUL, ESC, and other control bytes are labeled";
    } else if (activeView === "hex") {
      elements.payload.textContent = formatHex(selectedBytes);
      elements.payloadHint.textContent = "Offsets are byte offsets (hexadecimal)";
    } else {
      elements.payload.textContent = selectedText;
      elements.payloadHint.textContent = "Original decoded text";
    }
    elements.rawTab.classList.toggle("active", activeView === "raw");
    elements.visibleTab.classList.toggle("active", activeView === "visible");
    elements.hexTab.classList.toggle("active", activeView === "hex");
    elements.rawTab.setAttribute("aria-selected", String(activeView === "raw"));
    elements.visibleTab.setAttribute("aria-selected", String(activeView === "visible"));
    elements.hexTab.setAttribute("aria-selected", String(activeView === "hex"));
  }

  async function loadPayload(job) {
    selectedBytes = null;
    selectedText = "";
    elements.payload.textContent = "Loading payload…";
    try {
      const response = await fetch(`${api}/jobs/${encodeURIComponent(job.id)}/download`, { cache: "no-store" });
      if (response.status === 404) {
        clearSelection("This job was cleared or evicted before its payload loaded.");
        await refresh();
        return;
      }
      if (!response.ok) throw new Error(`Server returned HTTP ${response.status}.`);
      const payloadBytes = new Uint8Array(await response.arrayBuffer());
      const payloadText = new TextDecoder("utf-8").decode(payloadBytes);
      if (selectedId !== job.id) return;
      selectedBytes = payloadBytes;
      selectedText = payloadText;
      renderPayload();
    } catch (error) {
      if (selectedId !== job.id) return;
      elements.payload.textContent = "Payload could not be loaded.";
      elements.copyFeedback.textContent = `Unable to inspect this payload: ${error.message}`;
      elements.copyFeedback.className = "feedback error";
    }
  }

  function clearSelection(message) {
    selectedId = "";
    selectedBytes = null;
    selectedText = "";
    elements.detail.hidden = true;
    elements.noSelection.hidden = false;
    elements.noSelection.querySelector("p").textContent = message || "Captured text, control characters, and bytes are shown locally. Nothing here is sent to a printer.";
    elements.copyFeedback.textContent = "";
    elements.previewImage.removeAttribute("src");
    delete elements.previewImage.dataset.source;
    elements.previewImage.hidden = true;
    renderJobs();
  }

  async function selectJob(id) {
    const job = jobs.find((entry) => entry.id === id);
    if (!job) return;
    const changed = selectedId !== id;
    selectedId = id;
    if (changed) elements.labelIndex.value = String(job.previewLabelIndex || 0);
    elements.noSelection.hidden = true;
    elements.detail.hidden = false;
    elements.copyFeedback.textContent = "";
    renderJobs();
    renderSelectedMetadata(job);
    if (changed || selectedBytes === null) await loadPayload(job);
  }

  async function refresh() {
    if (loading) return;
    loading = true;
    try {
      const response = await fetch(`${api}/jobs`, { cache: "no-store" });
      if (!response.ok) throw new Error(`Server returned HTTP ${response.status}.`);
      const data = await response.json();
      if (!Array.isArray(data.jobs)) throw new Error("The server returned an invalid job list.");
      previewEnabled = data.previewEnabled === true;
      jobs = data.jobs.slice(0, 100);
      setConnection(true, "Connected · updating automatically");
      elements.lastUpdated.textContent = `Updated ${new Date().toLocaleTimeString()}`;
      renderJobs();
      if (!selectedId && jobs.length === 0) {
        elements.jobCount.textContent = "No retained jobs";
      } else if (!selectedId && jobs.length > 0 && elements.noSelection.hidden) {
        clearSelection();
      }
    } catch (error) {
      setConnection(false, `Disconnected · ${error.message} Reopen Inspector from print-bridge after changing its port.`);
      elements.jobCount.textContent = "History unavailable";
      elements.clearButton.disabled = true;
    } finally {
      loading = false;
    }
  }

  async function clearHistory() {
    if (jobs.length === 0 || !window.confirm("Clear all captured jobs from this app session?")) return;
    elements.clearButton.disabled = true;
    try {
      const response = await fetch(`${api}/jobs`, { method: "DELETE", cache: "no-store" });
      if (!response.ok) throw new Error(`Server returned HTTP ${response.status}.`);
      jobs = [];
      clearSelection("History was cleared. New jobs will appear here while Virtual mode is active.");
      renderJobs();
    } catch (error) {
      elements.copyFeedback.textContent = `Could not clear history: ${error.message}`;
      elements.copyFeedback.className = "feedback error";
      await refresh();
    }
  }

  async function copyText() {
    if (selectedBytes === null) return;
    try {
      if (!navigator.clipboard || typeof navigator.clipboard.writeText !== "function") {
        throw new Error("Clipboard access is unavailable in this browser or context.");
      }
      await navigator.clipboard.writeText(selectedText);
      elements.copyFeedback.textContent = "Raw text copied.";
      elements.copyFeedback.className = "feedback success";
    } catch (error) {
      elements.copyFeedback.textContent = `${error.message} Select Raw text and copy it manually.`;
      elements.copyFeedback.className = "feedback error";
    }
  }

  async function renderPreview() {
    if (!selectedId || !previewEnabled) return;
    const index = Number(elements.labelIndex.value);
    if (!Number.isInteger(index) || index < 0 || index > 99) {
      elements.previewMessage.hidden = false;
      elements.previewMessage.textContent = "Label index must be an integer from 0 to 99.";
      return;
    }
    elements.renderButton.disabled = true;
    try {
      const response = await fetch(`${api}/jobs/${encodeURIComponent(selectedId)}/preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ labelIndex: index }),
        cache: "no-store",
      });
      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.message || `Server returned HTTP ${response.status}.`);
      }
      elements.previewMessage.hidden = false;
      elements.previewMessage.textContent = `Rendering label ${index}…`;
      await refresh();
    } catch (error) {
      elements.previewMessage.hidden = false;
      elements.previewMessage.textContent = `Could not queue preview: ${error.message}`;
      elements.renderButton.disabled = false;
    }
  }

  elements.rawTab.addEventListener("click", () => { activeView = "raw"; renderPayload(); });
  elements.visibleTab.addEventListener("click", () => { activeView = "visible"; renderPayload(); });
  elements.hexTab.addEventListener("click", () => { activeView = "hex"; renderPayload(); });
  elements.copyButton.addEventListener("click", copyText);
  elements.renderButton.addEventListener("click", renderPreview);
  elements.labelIndex.addEventListener("input", () => {
    const job = jobs.find((entry) => entry.id === selectedId);
    if (job) updateRenderButton(job);
  });
  elements.clearButton.addEventListener("click", clearHistory);
  elements.previewImage.addEventListener("error", () => {
    elements.previewImage.hidden = true;
    elements.previewMessage.hidden = false;
    elements.previewMessage.textContent = "Preview could not be loaded. The job may have been cleared or evicted.";
  });

  refresh();
  window.setInterval(refresh, refreshMs);
})();
