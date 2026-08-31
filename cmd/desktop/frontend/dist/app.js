// No framework, no build step: Wails serves these files directly from the
// embedded frontend/dist folder. Every call into Go goes through
// window.go.main.App.<Method>(...), which Wails generates bindings for at
// build time based on the exported methods on App (see ../app.go).
//
// NOTE: don't name any top-level helper `go` — a global `function go(){}`
// declaration assigns to window.go just like Wails' own injected bindings
// do, and since this script loads after the runtime script, it silently
// clobbers window.go.main.App. (Learned the hard way — see backend() below.)

const state = {
  collections: [],
  activeCollectionName: null,
  ocrAvailable: false,
};

const els = {
  collectionList: document.getElementById("collection-list"),
  newCollectionBtn: document.getElementById("new-collection-btn"),
  newCollectionForm: document.getElementById("new-collection-form"),
  newCollectionName: document.getElementById("new-collection-name"),
  newCollectionPath: document.getElementById("new-collection-path"),
  browseBtn: document.getElementById("browse-btn"),
  cancelNewCollection: document.getElementById("cancel-new-collection"),
  createCollectionBtn: document.getElementById("create-collection-btn"),
  indexStatus: document.getElementById("index-status"),
  ocrBadge: document.getElementById("ocr-badge"),
  activeCollectionName: document.getElementById("active-collection-name"),
  activeCollectionMeta: document.getElementById("active-collection-meta"),
  reindexBtn: document.getElementById("reindex-btn"),
  conversation: document.getElementById("conversation"),
  askForm: document.getElementById("ask-form"),
  questionInput: document.getElementById("question-input"),
  askBtn: document.getElementById("ask-btn"),
};

function backend() {
  // window.go is injected by the Wails runtime once the app has loaded.
  return window.go.main.App;
}

// Wails populates window.go.main.App asynchronously shortly after the page
// loads (it ships as an empty window.go = {} first, then fills it in via a
// SetBindings call once the Go side is ready). On a cold start this can
// lose the race against our own script running immediately, so wait for
// the binding to actually exist before calling into it.
function waitForBindings(timeoutMs = 5000) {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    (function poll() {
      if (window.go && window.go.main && window.go.main.App) {
        resolve();
        return;
      }
      if (Date.now() - start > timeoutMs) {
        reject(new Error("Wails bindings did not become available in time"));
        return;
      }
      setTimeout(poll, 50);
    })();
  });
}

async function init() {
  try {
    await waitForBindings();
  } catch (e) {
    renderFatalError(String(e));
    return;
  }

  try {
    state.ocrAvailable = await backend().OCRAvailable();
  } catch (e) {
    state.ocrAvailable = false;
  }
  els.ocrBadge.classList.toggle("hidden", !state.ocrAvailable);

  await refreshCollections();
  renderEmptyConversation();
}

function renderFatalError(message) {
  els.collectionList.innerHTML = `<li class="empty-hint">Startup error: ${escapeHtml(message)}</li>`;
}async function refreshCollections() {
  try {
    state.collections = (await backend().ListCollections()) || [];
    state.lastListError = null;
  } catch (e) {
    state.collections = [];
    state.lastListError = String(e);
  }
  renderCollectionList();
}

function renderCollectionList() {
  els.collectionList.innerHTML = "";
  if (state.lastListError) {
    const li = document.createElement("li");
    li.className = "empty-hint";
    li.textContent = "Error loading collections: " + state.lastListError;
    els.collectionList.appendChild(li);
    return;
  }
  if (state.collections.length === 0) {
    const li = document.createElement("li");
    li.className = "empty-hint";
    li.textContent = "No collections yet — index a folder to get started.";
    els.collectionList.appendChild(li);
    return;
  }
  for (const c of state.collections) {
    const li = document.createElement("li");
    li.className = c.name === state.activeCollectionName ? "active" : "";
    li.innerHTML = `<span class="coll-name">${escapeHtml(c.name)}</span>` +
      `<span class="coll-meta">${c.docCount} docs · ${c.chunkCount} chunks</span>`;
    li.addEventListener("click", () => selectCollection(c));
    els.collectionList.appendChild(li);
  }
}

function selectCollection(c) {
  state.activeCollectionName = c.name;
  els.activeCollectionName.textContent = c.name;
  els.activeCollectionMeta.textContent =
    `${c.docCount} docs · ${c.chunkCount} chunks · ${c.rootPath}`;
  els.reindexBtn.classList.remove("hidden");
  els.reindexBtn.dataset.rootPath = c.rootPath;
  renderCollectionList();
  renderEmptyConversation();
}

function renderEmptyConversation() {
  els.conversation.innerHTML = "";
  const div = document.createElement("div");
  div.className = "empty-state";
  div.textContent = state.activeCollectionName
    ? "Ask a question about this collection — answers come with citations back to the source file."
    : "Select a collection on the left, or index a new folder, to start asking questions.";
  els.conversation.appendChild(div);
}

// --- new collection form ---------------------------------------------

els.newCollectionBtn.addEventListener("click", () => {
  els.newCollectionForm.classList.remove("hidden");
  els.newCollectionBtn.classList.add("hidden");
});

els.cancelNewCollection.addEventListener("click", () => {
  closeNewCollectionForm();
});

function closeNewCollectionForm() {
  els.newCollectionForm.classList.add("hidden");
  els.newCollectionBtn.classList.remove("hidden");
  els.newCollectionName.value = "";
  els.newCollectionPath.value = "";
  setIndexStatus("", "");
}

els.browseBtn.addEventListener("click", async () => {
  try {
    const path = await backend().ChooseFolder();
    if (path) els.newCollectionPath.value = path;
  } catch (e) {
    setIndexStatus("Couldn't open folder picker: " + e, "error");
  }
});

els.createCollectionBtn.addEventListener("click", async () => {
  const name = els.newCollectionName.value.trim();
  const path = els.newCollectionPath.value.trim();
  if (!name || !path) {
    setIndexStatus("Give the collection a name and a folder.", "error");
    return;
  }
  await indexFolder(name, path);
});

async function indexFolder(name, path) {
  els.createCollectionBtn.disabled = true;
  setIndexStatus("Indexing…", "");
  try {
    const res = await backend().IndexFolder(name, path);
    const total = res.added.length + res.updated.length;
    setIndexStatus(
      `Indexed: +${res.added.length} added, ${res.updated.length} updated, ` +
      `${res.unchanged.length} unchanged, ${res.skipped.length} skipped` +
      (Object.keys(res.errors || {}).length ? `, ${Object.keys(res.errors).length} errors` : ""),
      "success"
    );
    await refreshCollections();
    const updated = state.collections.find((c) => c.name === name);
    if (updated) selectCollection(updated);
    if (total >= 0) {
      setTimeout(closeNewCollectionForm, 900);
    }
  } catch (e) {
    setIndexStatus("Index failed: " + e, "error");
  } finally {
    els.createCollectionBtn.disabled = false;
  }
}

function setIndexStatus(text, kind) {
  els.indexStatus.textContent = text;
  els.indexStatus.className = "status-line" + (kind ? " " + kind : "");
}

els.reindexBtn.addEventListener("click", async () => {
  if (!state.activeCollectionName) return;
  const rootPath = els.reindexBtn.dataset.rootPath;
  els.reindexBtn.disabled = true;
  els.reindexBtn.textContent = "Re-indexing…";
  try {
    await backend().IndexFolder(state.activeCollectionName, rootPath);
    await refreshCollections();
  } finally {
    els.reindexBtn.disabled = false;
    els.reindexBtn.textContent = "Re-index";
  }
});

// --- asking questions --------------------------------------------------

els.askForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const question = els.questionInput.value.trim();
  if (!question || !state.activeCollectionName) return;

  if (els.conversation.querySelector(".empty-state")) {
    els.conversation.innerHTML = "";
  }

  const turn = document.createElement("div");
  turn.className = "qa-turn";
  turn.innerHTML = `<div class="q-bubble">${escapeHtml(question)}</div><div class="a-block">Thinking…</div>`;
  els.conversation.appendChild(turn);
  els.conversation.scrollTop = els.conversation.scrollHeight;

  els.questionInput.value = "";
  els.askBtn.disabled = true;

  try {
    const ans = await backend().AskQuestion(state.activeCollectionName, question, 5);
    renderAnswer(turn, question, ans);
  } catch (err) {
    turn.querySelector(".a-block").textContent = "Error: " + err;
  } finally {
    els.askBtn.disabled = false;
    els.conversation.scrollTop = els.conversation.scrollHeight;
  }
});

function renderAnswer(turn, question, ans) {
  const block = turn.querySelector(".a-block");
  block.classList.toggle("unanswerable", !ans.answerable);

  let citationsHtml = "";
  if (ans.answerable && ans.citations && ans.citations.length > 0) {
    citationsHtml = `<div class="citations">${ans.citations
      .map(
        (c) =>
          `<div class="citation"><span class="path">${escapeHtml(c.document_path)}</span>` +
          `<span>score ${c.score.toFixed(3)}</span></div>`
      )
      .join("")}</div>`;
  }

  block.innerHTML =
    `<div class="a-text">${escapeHtml(ans.text)}</div>` +
    citationsHtml +
    `<div class="a-footer">` +
    `<span>${escapeHtml(ans.backend)} · confidence ${ans.confidence.toFixed(3)}</span>` +
    (ans.answerable ? `<button class="btn btn-small btn-ghost export-btn">Export .md</button>` : "") +
    `</div>`;

  const exportBtn = block.querySelector(".export-btn");
  if (exportBtn) {
    exportBtn.addEventListener("click", async () => {
      try {
        const path = await backend().ExportSession(state.activeCollectionName, question, ans);
        if (path) {
          exportBtn.textContent = "Exported ✓";
          setTimeout(() => (exportBtn.textContent = "Export .md"), 1500);
        }
      } catch (e) {
        exportBtn.textContent = "Export failed";
      }
    });
  }
}

function escapeHtml(s) {
  const div = document.createElement("div");
  div.textContent = s == null ? "" : String(s);
  return div.innerHTML;
}

init();
window.addEventListener("error", (e) => {
  console.error("Unhandled error:", e.message);
});
console.log("HomeBred-RAG frontend loaded, window.go available:", !!window.go);
