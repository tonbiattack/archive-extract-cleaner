(() => {
  "use strict";

  const ROW_H = 34;
  const OVERSCAN = 10;
  const PAGE_SIZE = 200;
  const COLUMN_STORAGE_KEY = "archive-extract-cleaner.columnWidths.v1";
  const COLUMNS = [
    { min: 34, def: 34 },
    { min: 90, def: 106 },
    { min: 100, def: 170 },
    { min: 100, def: 190 },
    { min: 100, def: 210 },
    { min: 100, def: 220 },
    { min: 80, def: 96 },
    { min: 120, def: 260 },
  ];

  const state = {
    roots: [],
    filter: "all",
    search: "",
    sort: "verdict",
    desc: false,
    total: 0,
    pages: new Map(),
    loading: new Set(),
    generation: 0,
    initialLoaded: false,
    selected: new Map(),
    activeId: null,
    lastRowIndex: null,
    busy: false,
    scanning: false,
    deleting: false,
    summary: null,
    warnings: [],
    warningTotal: 0,
    logPath: "",
    configPath: "",
    configWarning: "",
    limits: null,
    multiDriveCopyScript: "",
    multiDriveCopyPreset: "",
    multiDriveCopyAvailable: false,
    deleteTotal: 0,
    columnWidths: COLUMNS.map((column) => column.def),
    contextRow: null,
    detailItem: null,
    transferCandidates: [],
    transferReview: null,
    transferRecommendedChecked: false,
    transferReviewSelected: new Set(),
    transferPaths: [],
    transferLaunched: false,
  };

  const el = (id) => document.getElementById(id);
  const statusText = el("statusText");
  const rootList = el("rootList");
  const rootInput = el("rootInput");
  const rootCount = el("rootCount");
  const scanBtn = el("scanBtn");
  const stopBtn = el("stopBtn");
  const browseBtn = el("browseBtn");
  const addRootBtn = el("addRootBtn");
  const searchInput = el("searchInput");
  const filterBar = el("filterBar");
  const summaryChips = el("summaryChips");
  const warningBadge = el("warningBadge");
  const tableScroll = el("tableScroll");
  const tableHeader = el("tableHeader");
  const rowsEl = el("rows");
  const virtualEl = el("virtual");
  const emptyState = el("emptyState");
  const detailPane = el("detailPane");
  const selectionInfo = el("selectionInfo");
  const deleteBtn = el("deleteBtn");
  const selectAllBtn = el("selectAllBtn");
  const clearSelectionBtn = el("clearSelectionBtn");
  const resetColumnsBtn = el("resetColumnsBtn");
  const transferBtn = el("transferBtn");
  const progressPanel = el("progressPanel");
  const progressLabel = el("progressLabel");
  const progressMeta = el("progressMeta");
  const progressFill = el("progressFill");
  const progressPath = el("progressPath");
  const modal = el("modal");
  const modalCard = el("modalCard");
  const modalTitle = el("modalTitle");
  const modalBody = el("modalBody");
  const modalActions = el("modalActions");
  const modalClose = el("modalClose");
  const toastEl = el("toast");
  const versionText = el("versionText");
  const settingsBtn = el("settingsBtn");
  const contextMenu = el("contextMenu");

  let toastTimer = null;
  let searchTimer = null;
  let renderScheduled = false;
  let lastProgressHide = null;
  let suppressSortClick = false;
  let measureContext = null;

  function esc(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function api(name, ...args) {
    const app = window.go && window.go.main && window.go.main.App;
    if (!app || typeof app[name] !== "function") {
      return Promise.reject(new Error("Wails API が利用できません"));
    }
    return Promise.resolve(app[name](...args));
  }

  function fmtBytes(bytes) {
    const n = Number(bytes) || 0;
    if (n <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let value = n;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit++;
    }
    const text = unit === 0 ? String(Math.round(value)) : value >= 100 ? value.toFixed(0) : value >= 10 ? value.toFixed(1) : value.toFixed(2);
    return text + " " + units[unit];
  }

  function fmtDuration(ms) {
    if (!ms || ms < 0) return "—";
    if (ms < 1000) return ms + " ms";
    const seconds = ms / 1000;
    if (seconds < 60) return seconds.toFixed(1) + " 秒";
    const minutes = Math.floor(seconds / 60);
    return minutes + " 分 " + Math.round(seconds % 60) + " 秒";
  }

  function fmtDate(value) {
    if (!value || String(value).startsWith("0001-")) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return date.toLocaleString("ja-JP");
  }

  function loadColumnWidths() {
    try {
      const raw = window.localStorage && window.localStorage.getItem(COLUMN_STORAGE_KEY);
      if (!raw) return;
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed) || parsed.length !== COLUMNS.length) return;
      state.columnWidths = COLUMNS.map((column, index) => {
        const value = Number(parsed[index]);
        return Number.isFinite(value) ? Math.min(900, Math.max(column.min, Math.round(value))) : column.def;
      });
    } catch (_) {
      state.columnWidths = COLUMNS.map((column) => column.def);
    }
  }

  function applyColumnWidths() {
    const total = state.columnWidths.reduce((sum, width) => sum + width, 0);
    const template = state.columnWidths.map((width) => width + "px").join(" ") + " minmax(0, 1fr)";
    const root = document.documentElement;
    if (root && root.style) {
      root.style.setProperty("--cols", template);
      root.style.setProperty("--min-table", total + "px");
    }
  }

  function saveColumnWidths() {
    try {
      if (window.localStorage) {
        window.localStorage.setItem(COLUMN_STORAGE_KEY, JSON.stringify(state.columnWidths));
      }
    } catch (_) {
      // 保存できない環境では現在のセッション内だけ保持する
    }
  }

  function resetColumnWidths() {
    state.columnWidths = COLUMNS.map((column) => column.def);
    applyColumnWidths();
    saveColumnWidths();
  }

  function columnText(row, index) {
    switch (index) {
      case 1:
        return VERDICT_LABEL[row.verdict] || row.verdict || "";
      case 2:
        return row.name || "";
      case 3:
        return row.zipPath ? baseName(row.zipPath) : "";
      case 4:
        return row.folderPath ? baseName(row.folderPath) : "";
      case 5:
        return row.parentPath || "";
      case 6:
        return fmtBytes(row.size);
      case 7:
        return row.reason || "";
      default:
        return "";
    }
  }

  function measureText(text) {
    if (!measureContext) {
      const canvas = document.createElement("canvas");
      measureContext = canvas.getContext ? canvas.getContext("2d") : null;
      if (measureContext) {
        measureContext.font = '12px "Segoe UI", "Yu Gothic UI", "Meiryo", system-ui, sans-serif';
      }
    }
    if (!measureContext) return String(text || "").length * 8;
    return measureContext.measureText(String(text || "")).width;
  }

  function autoFitColumn(index) {
    if (index < 1 || index >= COLUMNS.length) return;
    let widest = 0;
    state.pages.forEach((rows) => {
      rows.forEach((row) => {
        widest = Math.max(widest, measureText(columnText(row, index)));
      });
    });
    if (widest === 0) return;
    state.columnWidths[index] = Math.max(COLUMNS[index].min, Math.min(900, Math.ceil(widest + 24)));
    applyColumnWidths();
    saveColumnWidths();
  }

  function setupColumnResizers() {
    tableHeader.querySelectorAll(".col-resize").forEach((handle) => {
      handle.addEventListener("pointerdown", (event) => {
        if (event.button !== 0) return;
        const index = Number(handle.dataset.col);
        if (!Number.isInteger(index) || index < 1 || index >= COLUMNS.length) return;
        event.preventDefault();
        event.stopPropagation();
        const startX = event.clientX;
        const startWidth = state.columnWidths[index];
        suppressSortClick = true;
        document.body.classList.add("col-resizing");
        if (handle.setPointerCapture) {
          try {
            handle.setPointerCapture(event.pointerId);
          } catch (_) {
            // ポインタキャプチャ非対応でもウィンドウイベントで継続する
          }
        }
        const onMove = (moveEvent) => {
          const width = Math.max(COLUMNS[index].min, Math.round(startWidth + moveEvent.clientX - startX));
          state.columnWidths[index] = width;
          applyColumnWidths();
        };
        const onUp = () => {
          window.removeEventListener("pointermove", onMove);
          window.removeEventListener("pointerup", onUp);
          window.removeEventListener("pointercancel", onUp);
          document.body.classList.remove("col-resizing");
          saveColumnWidths();
          setTimeout(() => {
            suppressSortClick = false;
          }, 0);
        };
        window.addEventListener("pointermove", onMove);
        window.addEventListener("pointerup", onUp);
        window.addEventListener("pointercancel", onUp);
      });
      handle.addEventListener("dblclick", (event) => {
        event.preventDefault();
        event.stopPropagation();
        autoFitColumn(Number(handle.dataset.col));
      });
    });
  }

  async function copyText(text) {
    if (!text) {
      toast("コピーする内容がありません", true);
      return;
    }
    try {
      if (typeof navigator !== "undefined" && navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text);
        toast("コピーしました");
        return;
      }
    } catch (_) {
      // Wails 側のクリップボードAPIへフォールバックする
    }
    try {
      await api("CopyText", text);
      toast("コピーしました");
    } catch (error) {
      toast("コピーに失敗しました: " + error.message, true);
    }
  }

  function showRowContextMenu(event, row) {
    state.contextRow = row;
    contextMenu.querySelectorAll("[data-copy-field]").forEach((button) => {
      button.disabled = !row[button.dataset.copyField];
    });
    const reveal = contextMenu.querySelector('[data-menu-action="reveal"]');
    if (reveal) reveal.disabled = !row.zipPath && !row.folderPath;
    contextMenu.classList.remove("hidden");
    const rect = contextMenu.getBoundingClientRect();
    let left = event.clientX;
    let top = event.clientY;
    if (left + rect.width > window.innerWidth - 8) left = Math.max(8, window.innerWidth - rect.width - 8);
    if (top + rect.height > window.innerHeight - 8) top = Math.max(8, window.innerHeight - rect.height - 8);
    contextMenu.style.left = left + "px";
    contextMenu.style.top = top + "px";
  }

  function hideContextMenu() {
    state.contextRow = null;
    contextMenu.classList.add("hidden");
  }

  const VERDICT_LABEL = {
    DELETE_CANDIDATE: "削除候補",
    MISMATCH: "不一致",
    UNVERIFIED: "未検証",
    AMBIGUOUS: "曖昧",
    KEEP_ARCHIVE: "ZIPのみ保持",
    KEEP_DIRECTORY: "フォルダのみ保持",
  };

  const MATCH_LABEL = {
    direct: "直接比較",
    "strip-top": "トップ階層を除去して比較",
  };

  function verdictBadge(item) {
    const verdict = item.verdict || item.Verdict || "";
    let html = '<span class="badge v-' + esc(verdict) + '">' + esc(VERDICT_LABEL[verdict] || verdict) + "</span>";
    const status = item.status || item.Status || "";
    if (status === "deleted") {
      html += '<span class="badge badge-deleted">ごみ箱へ移動済み</span>';
    } else if (status === "error") {
      html += '<span class="badge v-UNVERIFIED">エラー</span>';
    }
    return html;
  }

  function toast(message, isError) {
    toastEl.textContent = message;
    toastEl.classList.toggle("error", !!isError);
    toastEl.classList.remove("hidden");
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toastEl.classList.add("hidden"), 6500);
  }

  function setStatus(message) {
    statusText.textContent = message;
  }

  // ---------- roots ----------

  function addRoot(path) {
    const value = String(path || "").trim().replace(/^"|"$/g, "");
    if (!value) return;
    if (state.busy) return;
    const exists = state.roots.some((root) => root.toLowerCase() === value.toLowerCase());
    if (exists) {
      toast("同じフォルダが既に追加されています");
      return;
    }
    state.roots.push(value);
    rootInput.value = "";
    renderRoots();
    updateBusyUI();
  }

  function renderRoots() {
    rootCount.textContent = state.roots.length ? state.roots.length + " 件" : "";
    rootList.innerHTML = state.roots
      .map(
        (root, index) =>
          '<span class="root-chip" title="' +
          esc(root) +
          '"><span class="root-path">' +
          esc(root) +
          '</span><button class="remove-btn" data-remove-root="' +
          index +
          '" aria-label="削除">×</button></span>'
      )
      .join("");
    rootList.querySelectorAll("[data-remove-root]").forEach((button) => {
      button.disabled = state.busy;
    });
  }

  // ---------- rows / virtual scroll ----------

  function currentQuery(offset, limit) {
    return {
      search: state.search,
      filter: state.filter,
      sort: state.sort,
      desc: state.desc,
      offset: offset || 0,
      limit: limit || PAGE_SIZE,
    };
  }

  function getRow(index) {
    const pageIndex = Math.floor(index / PAGE_SIZE);
    const page = state.pages.get(pageIndex);
    if (!page) return null;
    return page[index % PAGE_SIZE] || null;
  }

  function scheduleRender() {
    if (renderScheduled) return;
    renderScheduled = true;
    requestAnimationFrame(() => {
      renderScheduled = false;
      renderRows();
    });
  }

  function renderRows() {
    const total = state.total;
    virtualEl.style.height = Math.max(0, total) * ROW_H + "px";

    if (!total) {
      rowsEl.innerHTML = "";
      emptyState.classList.remove("hidden");
      updateEmptyState();
      return;
    }
    emptyState.classList.add("hidden");

    const scrollTop = tableScroll.scrollTop;
    const viewport = tableScroll.clientHeight;
    const start = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN);
    const visibleCount = Math.ceil(viewport / ROW_H) + OVERSCAN * 2;
    const end = Math.min(total, start + visibleCount);

    rowsEl.style.transform = "translate3d(0," + start * ROW_H + "px,0)";

    const html = [];
    for (let index = start; index < end; index++) {
      const row = getRow(index);
      html.push(row ? rowHtml(row, index) : placeholderHtml(index));
    }
    rowsEl.innerHTML = html.join("");

    const firstPage = Math.floor(start / PAGE_SIZE);
    const lastPage = Math.floor(Math.max(start, end - 1) / PAGE_SIZE);
    ensurePages(firstPage, lastPage);
  }

  function updateEmptyState() {
    let title = "まだスキャンしていません";
    let text = "対象フォルダを追加して「スキャン」を押してください。スキャンは読み取り専用です。";
    if (state.busy && state.scanning) {
      title = "スキャン中です";
      text = "ZIPと解凍済みフォルダを照合しています。完了までお待ちください。";
    } else if (state.busy) {
      title = "読み込み中です";
      text = "結果を取得しています。";
    } else if (state.summary && state.summary.total > 0) {
      title = "条件に一致する項目がありません";
      text = "検索語や判定フィルタを変更してください。";
    } else if (state.roots.length === 0) {
      title = "対象フォルダが未指定です";
      text = "上部の入力欄に対象フォルダを追加するか、「フォルダを選択」を押してください。";
    } else if (state.initialLoaded) {
      title = "該当する ZIP / フォルダはありません";
      text = "対象フォルダには、対応するZIPまたは解凍済みフォルダが見つかりませんでした。";
    }
    emptyState.innerHTML =
      '<div class="empty-title">' + esc(title) + '</div><div class="empty-text">' + esc(text) + "</div>";
  }

  function rowHtml(row, index) {
    const selected = state.selected.has(row.id);
    const active = state.activeId === row.id;
    const classes = ["row"];
    if (selected) classes.push("selected");
    if (active) classes.push("active");
    if (row.status === "deleted") classes.push("deleted");
    const checkbox = row.deletable
      ? '<input type="checkbox" data-role="select" data-id="' +
        esc(row.id) +
        '" ' +
        (selected ? "checked" : "") +
        ">"
      : '<span class="muted" title="削除候補ではないため選択できません">—</span>';
    const zipName = row.zipPath ? baseName(row.zipPath) : "";
    const folderName = row.folderPath ? baseName(row.folderPath) : "";
    return (
      '<div class="' +
      classes.join(" ") +
      '" data-index="' +
      index +
      '" data-id="' +
      esc(row.id) +
      '">' +
      '<div class="cell check">' +
      checkbox +
      "</div>" +
      '<div class="cell">' +
      verdictBadge(row) +
      "</div>" +
      '<div class="cell strong" title="' +
      esc(row.name) +
      '">' +
      esc(row.name || "—") +
      "</div>" +
      '<div class="cell path-cell" title="' +
      esc(row.zipPath) +
      '">' +
      esc(zipName || "—") +
      "</div>" +
      '<div class="cell path-cell" title="' +
      esc(row.folderPath) +
      '">' +
      esc(folderName || "—") +
      "</div>" +
      '<div class="cell path-cell" title="' +
      esc(row.parentPath) +
      '">' +
      esc(row.parentPath || "—") +
      "</div>" +
      '<div class="cell numeric">' +
      esc(fmtBytes(row.size)) +
      "</div>" +
      '<div class="cell reason-cell" title="' +
      esc(row.reason) +
      '">' +
      esc(row.reason || "—") +
      "</div>" +
      "</div>"
    );
  }

  function placeholderHtml(index) {
    return (
      '<div class="row placeholder" data-index="' +
      index +
      '"><div class="cell check"></div><div class="cell">読み込み中…</div>' +
      '<div class="cell muted-cell">—</div><div class="cell"></div><div class="cell"></div>' +
      '<div class="cell"></div><div class="cell"></div><div class="cell"></div></div>'
    );
  }

  function baseName(path) {
    if (!path) return "";
    const normalized = String(path).replace(/[\\/]+$/, "");
    const index = Math.max(normalized.lastIndexOf("\\"), normalized.lastIndexOf("/"));
    return index >= 0 ? normalized.slice(index + 1) : normalized;
  }

  function ensurePages(firstPage, lastPage) {
    const pagesToLoad = [];
    for (let page = firstPage; page <= lastPage; page++) {
      if (!state.pages.has(page) && !state.loading.has(page)) pagesToLoad.push(page);
    }
    if (pagesToLoad.length === 0) return;
    const maxConcurrent = 3;
    for (let i = 0; i < Math.min(maxConcurrent, pagesToLoad.length); i++) {
      fetchPage(pagesToLoad[i]);
    }
    if (pagesToLoad.length > maxConcurrent) {
      setTimeout(() => ensurePages(firstPage, lastPage), 120);
    }
  }

  function fetchPage(pageIndex) {
    if (state.pages.has(pageIndex) || state.loading.has(pageIndex)) return;
    state.loading.add(pageIndex);
    const generation = state.generation;
    api("GetRows", currentQuery(pageIndex * PAGE_SIZE, PAGE_SIZE))
      .then((page) => {
        if (generation !== state.generation) return;
        state.pages.set(pageIndex, page.rows || []);
        state.total = Number(page.total) || 0;
        state.initialLoaded = true;
        applyMeta(page);
        renderRows();
      })
      .catch((error) => {
        if (generation === state.generation) {
          toast("一覧の取得に失敗しました: " + error.message, true);
        }
      })
      .finally(() => {
        state.loading.delete(pageIndex);
      });
  }

  function applyMeta(page) {
    if (page.summary) {
      state.summary = page.summary;
    }
    if (Array.isArray(page.roots)) {
      state.roots = page.roots;
      renderRoots();
    }
    if (Array.isArray(page.warnings)) {
      state.warnings = page.warnings;
      state.warningTotal = Number(page.warningTotal) || page.warnings.length;
      renderWarnings();
    }
    renderSummary();
  }

  function clearRows(resetScroll) {
    state.generation++;
    state.pages.clear();
    state.loading.clear();
    state.total = 0;
    state.initialLoaded = false;
    state.lastRowIndex = null;
    if (resetScroll) tableScroll.scrollTop = 0;
    updateEmptyState();
    renderRows();
  }

  function reloadRows(resetScroll) {
    clearRows(resetScroll);
    if (state.scanning) return;
    fetchPage(0);
  }

  // ---------- selection ----------

  function addSelection(row) {
    if (!row || !row.deletable) return;
    state.selected.set(row.id, {
      id: row.id,
      name: row.name,
      size: row.size,
      zipPath: row.zipPath,
      folderPath: row.folderPath,
    });
  }

  function removeSelection(id) {
    state.selected.delete(id);
  }

  function rangeSelect(index) {
    const from = state.lastRowIndex == null ? index : state.lastRowIndex;
    const start = Math.min(from, index);
    const end = Math.max(from, index);
    let added = 0;
    for (let i = start; i <= end; i++) {
      const row = getRow(i);
      if (row && row.deletable) {
        addSelection(row);
        added++;
      }
    }
    state.lastRowIndex = index;
    updateSelectionUI();
    renderRows();
    if (added === 0) toast("範囲内に選択可能な削除候補がありません");
  }

  function updateSelectionUI() {
    let bytes = 0;
    state.selected.forEach((item) => {
      bytes += Number(item.size) || 0;
    });
    selectionInfo.textContent = "選択 " + state.selected.size + " 件 / " + fmtBytes(bytes);
    selectionInfo.title = "選択は検索・フィルタを切り替えても保持されます。新しいスキャンで解除されます。";
    deleteBtn.disabled = state.busy || state.selected.size === 0;
    clearSelectionBtn.disabled = state.selected.size === 0;
  }

  async function selectAllCandidates() {
    if (state.busy) return;
    selectAllBtn.disabled = true;
    try {
      const ids = await api("CandidateIDs", currentQuery(0, 0));
      if (!ids || ids.length === 0) {
        toast("現在の条件で選択できる削除候補がありません");
        return;
      }
      const preview = await api("PreviewDelete", ids);
      state.selected.clear();
      (preview.items || []).forEach((item) => {
        state.selected.set(item.id, {
          id: item.id,
          name: item.name,
          size: item.size,
          zipPath: item.zipPath,
          folderPath: item.folderPath,
        });
      });
      state.lastRowIndex = null;
      updateSelectionUI();
      renderRows();
      toast(state.selected.size + " 件の削除候補を選択しました");
    } catch (error) {
      toast("選択に失敗しました: " + error.message, true);
    } finally {
      selectAllBtn.disabled = state.busy;
    }
  }

  // ---------- summary / warnings ----------

  function renderSummary() {
    const summary = state.summary;
    if (!summary) {
      summaryChips.innerHTML = '<span class="chip">総件数 <strong>0</strong></span>';
      updateWorkflow();
      return;
    }
    const byVerdict = summary.byVerdict || {};
    const keep = (byVerdict.KEEP_ARCHIVE || 0) + (byVerdict.KEEP_DIRECTORY || 0);
    const unverified = (byVerdict.UNVERIFIED || 0) + (byVerdict.AMBIGUOUS || 0);
    const chips = [
      '<span class="chip">総件数 <strong>' + (summary.total || 0) + "</strong></span>",
      '<span class="chip candidate">削除候補 <strong>' +
        (summary.candidateCount || 0) +
        "</strong> / " +
        esc(fmtBytes(summary.candidateBytes || 0)) +
        "</span>",
      '<span class="chip">不一致 <strong>' + (byVerdict.MISMATCH || 0) + "</strong></span>",
      '<span class="chip">未検証・曖昧 <strong>' + unverified + "</strong></span>",
      '<span class="chip">保持 <strong>' + keep + "</strong></span>",
    ];
    if (summary.deletedCount) {
      chips.push('<span class="chip">移動済み <strong>' + summary.deletedCount + "</strong></span>");
    }
    if (summary.durationMs) {
      chips.push('<span class="chip">所要時間 <strong>' + esc(fmtDuration(summary.durationMs)) + "</strong></span>");
    }
    summaryChips.innerHTML = chips.join("");
    updateWorkflow();
  }

  function hasScanResult() {
    const summary = state.summary;
    return !!(summary && summary.scannedAt && !String(summary.scannedAt).startsWith("0001-"));
  }

  function setWorkflowStep(bar, step, stateName) {
    const element = bar.querySelector('[data-step="' + step + '"]');
    if (!element) return;
    element.classList.remove("active", "done");
    if (stateName) element.classList.add(stateName);
  }

  function updateWorkflow() {
    const bar = document.getElementById("workflowBar");
    if (!bar) return;
    const scanned = hasScanResult();
    const launched = state.transferLaunched;
    setWorkflowStep(bar, "organize", scanned ? "done" : "active");
    setWorkflowStep(bar, "transfer", scanned ? "active" : "");
    setWorkflowStep(bar, "cleanup", "");
    const current = document.getElementById("workflowCurrent");
    if (current) {
      current.textContent = launched
        ? "multi-drive-copyで転送・検証中。結果は受信側で確認"
        : scanned
        ? "転送対象をレビューしてください"
        : "対象フォルダをスキャンしてください";
    }
  }

  function renderWarnings() {
    if (!state.warningTotal) {
      warningBadge.classList.add("hidden");
      return;
    }
    warningBadge.classList.remove("hidden");
    warningBadge.textContent = "警告 " + state.warningTotal + " 件";
    warningBadge.title = (state.warnings || []).slice(0, 20).join("\n") +
      (state.warningTotal > state.warnings.length ? "\nほか " + (state.warningTotal - state.warnings.length) + " 件" : "");
  }

  function showWarnings() {
    if (!state.warningTotal) return;
    const items = (state.warnings || []).slice(0, 200);
    const rest = state.warningTotal - items.length;
    openModal({
      title: "走査中の警告",
      wide: true,
      body:
        '<p class="muted">読み取りできない項目やリンク/ジャンクションは安全側でスキップしました。該当フォルダは削除候補になりません。</p>' +
        '<ul class="modal-list">' +
        items.map((item) => "<li>" + esc(item) + "</li>").join("") +
        (rest > 0 ? '<li class="muted">ほか ' + rest + " 件</li>" : "") +
        "</ul>",
      actions: [{ label: "閉じる", className: "btn", onClick: closeModal }],
    });
  }

  function showSettings() {
    const limits = state.limits || {};
    let body =
      '<dl class="detail-grid">' +
      "<dt>設定ファイル</dt><dd>" + esc(state.configPath || "—") + "</dd>" +
      "<dt>操作ログ</dt><dd>" + esc(state.logPath || "—") + "</dd>" +
      "<dt>最大エントリ数</dt><dd>" + esc(limits.maxEntries || "—") + "</dd>" +
      "<dt>合計展開サイズ上限</dt><dd>" + esc(limits.maxTotalBytes ? fmtBytes(limits.maxTotalBytes) : "—") + "</dd>" +
      "<dt>単一ファイル上限</dt><dd>" + esc(limits.maxFileBytes ? fmtBytes(limits.maxFileBytes) : "—") + "</dd>" +
      "<dt>圧縮率上限</dt><dd>" + esc(limits.maxCompressionRatio || "—") + "</dd>" +
      "<dt>multi-drive-copy</dt><dd>" + esc(state.multiDriveCopyScript || "—") + (state.multiDriveCopyAvailable ? "" : "（未検出）") + "</dd>" +
      "<dt>転送プリセット</dt><dd>" + esc(state.multiDriveCopyPreset || "設定画面を開く") + "</dd>" +
      "</dl>" +
      '<p class="modal-note">上限を超えたZIPやフォルダは UNVERIFIED となり、削除候補にはなりません。設定ファイルを変更した場合はアプリを再起動してください。</p>' +
      '<p class="modal-note">multi-drive-copy のスクリプトとプリセットは設定ファイルの <code>multiDriveCopy</code> で指定します。プリセット未指定時は multi-drive-copy の設定画面が開きます。</p>';
    if (state.configWarning) {
      body += '<p class="modal-note">' + esc(state.configWarning) + "</p>";
    }
    openModal({
      title: "設定と上限値",
      body,
      actions: [
        {
          label: "設定ファイルを開く",
          className: "btn",
          onClick: () => api("OpenConfig").catch((error) => toast("設定ファイルを開けません: " + error.message, true)),
        },
        { label: "閉じる", className: "btn primary", onClick: closeModal },
      ],
    });
  }

  // ---------- detail ----------

  async function showDetail(id) {
    state.activeId = id;
    renderRows();
    try {
      const item = await api("GetItemDetail", id);
      renderDetail(item);
    } catch (error) {
      toast("詳細の取得に失敗しました: " + error.message, true);
    }
  }

  function renderDetail(item) {
    state.detailItem = item;
    const rows = [];
    rows.push(["判定", verdictBadge(item)]);
    if (item.status === "error") {
      rows.push(["状態", '<span class="result-fail">エラー</span>']);
    } else if (item.status === "deleted") {
      rows.push(["状態", '<span class="result-ok">ごみ箱へ移動済み</span>']);
    }
    rows.push(["ZIP", item.zipPath || "—"]);
    rows.push(["解凍済みフォルダ", item.folderPath || "—"]);
    rows.push(["親パス", item.parentPath || "—"]);
    rows.push(["対象サイズ", fmtBytes(item.size)]);
    if (item.zipSize) rows.push(["ZIPサイズ", fmtBytes(item.zipSize)]);
    if (item.folderSize) rows.push(["フォルダサイズ", fmtBytes(item.folderSize)]);
    if (item.matchMode) rows.push(["一致方式", MATCH_LABEL[item.matchMode] || item.matchMode]);
    if (item.fileCount || item.dirCount) {
      rows.push(["ファイル / ディレクトリ", (item.fileCount || 0) + " / " + (item.dirCount || 0)]);
    }
    if (item.zipEntries) rows.push(["ZIPエントリ数", String(item.zipEntries)]);
    if (item.comparedBytes) rows.push(["比較バイト数", fmtBytes(item.comparedBytes)]);
    rows.push(["確認日時", fmtDate(item.checkedAt)]);
    if (item.deletedAt && !String(item.deletedAt).startsWith("0001-")) {
      rows.push(["移動日時", fmtDate(item.deletedAt)]);
    }
    if (item.deleteError) rows.push(["エラー", item.deleteError]);

    const copyActions = [
      ["name", "名前をコピー"],
      ["zipPath", "ZIPパスをコピー"],
      ["folderPath", "フォルダパスをコピー"],
      ["parentPath", "親パスをコピー"],
    ];
    const copyButtons = copyActions
      .map(
        ([field, label]) =>
          '<button class="copy-btn" data-action="copy" data-field="' +
          field +
          '"' +
          (item[field] ? "" : " disabled") +
          ">" +
          esc(label) +
          "</button>"
      )
      .join("");

    let html =
      '<div class="detail-block"><h3 class="detail-name">' +
      esc(item.name) +
      "</h3><p class=\"detail-reason\">" +
      esc(item.reason || "") +
      "</p>" +
      '<div class="detail-actions">' +
      '<button class="btn small" data-action="reveal" data-id="' +
      esc(item.id) +
      '">エクスプローラーで表示</button>' +
      copyButtons +
      "</div>" +
      '<dl class="detail-grid">' +
      rows
        .map(
          ([label, value]) =>
            "<dt>" + esc(label) + "</dt><dd>" + (label === "判定" || label === "状態" ? value : esc(value)) + "</dd>"
        )
        .join("") +
      "</dl></div>";

    if (Array.isArray(item.details) && item.details.length) {
      html +=
        '<div class="detail-block"><div class="detail-title">差分・根拠</div><ul class="detail-list">' +
        item.details.map((detail) => "<li>" + esc(detail) + "</li>").join("") +
        "</ul></div>";
    }
    html +=
      '<div class="detail-note">MVPでは解凍済みフォルダのみWindowsのごみ箱へ移動できます。ZIP本体は移動されません。失敗時に完全削除へフォールバックしません。</div>';
    detailPane.innerHTML = html;
  }

  // ---------- progress ----------

  function renderProgress(progress) {
    progressPanel.classList.remove("hidden");
    const phase = progress.phase;
    if (phase === "walking") {
      progressLabel.textContent = "フォルダを走査中";
      progressFill.classList.add("indeterminate");
      progressFill.style.width = "32%";
      progressMeta.textContent =
        "フォルダ " +
        (progress.walkedDirs || 0) +
        " / ZIP " +
        (progress.archives || 0) +
        " / " +
        fmtDuration(progress.elapsedMs);
    } else if (phase === "verifying") {
      progressLabel.textContent = "ZIPとフォルダを照合中";
      progressFill.classList.remove("indeterminate");
      const total = Number(progress.total) || 0;
      const completed = Number(progress.completed) || 0;
      const percent = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0;
      progressFill.style.width = percent + "%";
      progressMeta.textContent =
        completed +
        " / " +
        total +
        " 件 / " +
        fmtBytes(progress.bytes || 0) +
        " / " +
        fmtDuration(progress.elapsedMs);
    } else if (phase === "done") {
      progressLabel.textContent = "照合が完了しました";
      progressFill.classList.remove("indeterminate");
      progressFill.style.width = "100%";
      progressMeta.textContent =
        (progress.completed || 0) +
        " 件 / " +
        fmtBytes(progress.bytes || 0) +
        " / " +
        fmtDuration(progress.elapsedMs);
    } else {
      progressLabel.textContent = "処理中";
      progressFill.classList.add("indeterminate");
      progressMeta.textContent = fmtDuration(progress.elapsedMs);
    }
    progressPath.textContent = progress.current || "";
  }

  function hideProgressSoon() {
    if (lastProgressHide) clearTimeout(lastProgressHide);
    lastProgressHide = setTimeout(() => {
      if (!state.scanning) progressPanel.classList.add("hidden");
    }, 1400);
  }

  // ---------- scanning ----------

  async function startScan() {
    if (state.busy) return;
    if (state.roots.length === 0) {
      toast("対象フォルダを1つ以上追加してください", true);
      rootInput.focus();
      return;
    }
    state.busy = true;
    state.scanning = true;
    state.selected.clear();
    state.activeId = null;
    state.summary = null;
    state.transferLaunched = false;
    state.warnings = [];
    state.warningTotal = 0;
    renderWarnings();
    renderSummary();
    detailPane.innerHTML =
      '<div class="detail-empty"><div class="detail-title">詳細</div><p>行を選択すると、ZIPとフォルダの対応・一致根拠・差異を表示します。</p></div>';
    state.detailItem = null;
    clearRows(true);
    progressPanel.classList.remove("hidden");
    progressLabel.textContent = "スキャンを開始しています";
    progressMeta.textContent = "";
    progressPath.textContent = "";
    progressFill.classList.remove("indeterminate");
    progressFill.style.width = "0";
    setStatus("スキャン中...");
    updateBusyUI();
    updateSelectionUI();
    try {
      const info = await api("Scan", state.roots);
      state.scanning = false;
      if (info && info.cancelled) {
        setStatus("スキャンを中断しました");
        toast("スキャンを中断しました");
        reloadRows(true);
      } else if (info) {
        if (info.summary) state.summary = info.summary;
        if (Array.isArray(info.roots)) state.roots = info.roots;
        state.warnings = info.warnings || [];
        state.warningTotal = info.warningTotal || state.warnings.length;
        renderRoots();
        renderSummary();
        renderWarnings();
        setStatus(
          "スキャン完了: " +
            (info.summary.total || 0) +
            " 件 / 削除候補 " +
            (info.summary.candidateCount || 0) +
            " 件 / " +
            fmtBytes(info.summary.candidateBytes || 0)
        );
        hideProgressSoon();
        reloadRows(true);
      }
    } catch (error) {
      state.scanning = false;
      progressPanel.classList.add("hidden");
      setStatus("スキャンに失敗しました");
      toast("スキャンに失敗しました: " + error.message, true);
      reloadRows(true);
    } finally {
      state.busy = false;
      updateBusyUI();
      updateSelectionUI();
    }
  }

  // ---------- delete ----------

  async function beginDelete() {
    if (state.busy || state.selected.size === 0) return;
    const ids = Array.from(state.selected.keys());
    state.busy = true;
    updateBusyUI();
    setStatus("ごみ箱に移動する対象を確認しています...");
    try {
      const preview = await api("PreviewDelete", ids);
      if (!preview || preview.count === 0) {
        toast("現在選択されている項目に移動可能な候補がありません", true);
        setStatus("選択項目に移動可能な候補がありません");
        return;
      }
      showDeleteConfirm(preview);
    } catch (error) {
      toast("移動対象の確認に失敗しました: " + error.message, true);
    } finally {
      state.busy = false;
      updateBusyUI();
      updateSelectionUI();
    }
  }

  function showDeleteConfirm(preview) {
    const items = preview.items || [];
    const visible = items.slice(0, 100);
    const rest = items.length - visible.length;
    const validIds = items.map((item) => item.id);
    const body =
      '<div class="stat-row">' +
      '<div class="stat"><div class="stat-label">移動対象</div><div class="stat-value">' + preview.count + " 件</div></div>" +
      '<div class="stat"><div class="stat-label">合計サイズ</div><div class="stat-value">' + esc(fmtBytes(preview.bytes)) + "</div></div>" +
      "</div>" +
      '<p class="modal-note">Windowsのごみ箱に移動します。元に戻せます。移動直前に各ZIPとフォルダを再照合し、内容に変化があればその対象はスキップされます。ZIP本体は移動されません。</p>' +
      '<div class="detail-title" style="margin-top:14px">ごみ箱に移動する解凍済みフォルダ</div>' +
      '<ul class="modal-list">' +
      visible.map((item) => "<li>" + esc(item.folderPath) + "</li>").join("") +
      (rest > 0 ? '<li class="muted">ほか ' + rest + " 件</li>" : "") +
      "</ul>";
    openModal({
      title: "ごみ箱への移動確認",
      body,
      wide: true,
      actions: [
        { label: "キャンセル", className: "btn", onClick: closeModal },
        {
          label: "ごみ箱に移動",
          className: "btn danger",
          onClick: () => executeDelete(validIds),
        },
      ],
    });
  }

  function deleteProgressBody(progress) {
    const total = state.deleteTotal || progress.total || 0;
    const index = progress.index || 0;
    const percent = total > 0 ? Math.min(100, Math.round((index / total) * 100)) : 0;
    const stageLabel =
      progress.stage === "revalidating"
        ? "再検証中"
        : progress.stage === "recycling" || progress.stage === "deleting"
        ? "ごみ箱へ移動中"
        : progress.stage === "skipped"
        ? "スキップ"
        : progress.stage === "failed"
        ? "失敗"
        : progress.stage === "done"
        ? "完了"
        : "処理中";
    return (
      '<p>対象を1件ずつ再検証してからWindowsのごみ箱に移動しています。アプリを閉じたり操作を中断しないでください。</p>' +
      '<div class="delete-progress"><div class="progress-head"><span>' +
      esc(stageLabel) +
      " " +
      index +
      " / " +
      total +
      '</span><span class="muted">' +
      esc(progress.name || "") +
      "</span></div>" +
      '<div class="progress-track"><div class="progress-fill" style="width:' +
      percent +
      '%"></div></div></div>'
    );
  }

  async function executeDelete(ids) {
    if (!ids || ids.length === 0) return;
    state.busy = true;
    state.deleting = true;
    state.deleteTotal = ids.length;
    updateBusyUI();
    setStatus("ごみ箱に移動しています...");
    openModal({
      title: "ごみ箱に移動しています",
      body: deleteProgressBody({ index: 0, total: ids.length, stage: "revalidating", name: "" }),
      actions: [],
      closable: false,
    });
    try {
      const response = await api("DeleteSelected", ids, true);
      state.deleting = false;
      if (response && response.summary) state.summary = response.summary;
      renderSummary();
      state.selected.clear();
      state.activeId = null;
      updateSelectionUI();
      await reloadRows(false);
      if (response && response.deleted > 0) {
        toast(response.deleted + " 件の解凍済みフォルダをごみ箱に移動しました");
        setStatus(
          "移動完了: 成功 " +
            response.deleted +
            " / 失敗 " +
            response.failed +
            " / スキップ " +
            response.skipped
        );
      } else {
        setStatus("ごみ箱に移動された項目はありません");
      }
      showDeleteResult(response);
    } catch (error) {
      state.deleting = false;
      closeModal();
      setStatus("ごみ箱への移動に失敗しました");
      toast("ごみ箱への移動に失敗しました: " + error.message, true);
    } finally {
      state.busy = false;
      updateBusyUI();
      updateSelectionUI();
    }
  }

  function showDeleteResult(response) {
    if (!response) return;
    const results = response.results || [];
    const failures = results.filter((item) => !item.success && !item.skipped);
    const skipped = results.filter((item) => item.skipped);
    let body =
      '<div class="stat-row">' +
      '<div class="stat"><div class="stat-label">移動成功</div><div class="stat-value result-ok">' +
      (response.deleted || 0) +
      "</div></div>" +
      '<div class="stat"><div class="stat-label">失敗</div><div class="stat-value result-fail">' +
      (response.failed || 0) +
      "</div></div>" +
      '<div class="stat"><div class="stat-label">スキップ</div><div class="stat-value">' +
      (response.skipped || 0) +
      "</div></div></div>";
    if (failures.length) {
      body +=
        '<div class="detail-title" style="margin-top:14px">失敗した項目</div><ul class="modal-list">' +
        failures
          .slice(0, 100)
          .map((item) => "<li><strong>" + esc(item.name || item.folderPath) + "</strong><br>" + esc(item.reason) + "</li>")
          .join("") +
        "</ul>";
    }
    if (skipped.length) {
      body +=
        '<div class="detail-title" style="margin-top:14px">スキップした項目</div><ul class="modal-list">' +
        skipped
          .slice(0, 50)
          .map((item) => "<li>" + esc(item.name || item.folderPath || item.id) + " — " + esc(item.reason) + "</li>")
          .join("") +
        "</ul>";
    }
    body +=
      '<p class="muted" style="margin-top:10px">操作履歴: ' +
      esc(state.logPath || "未設定") +
      "</p>";
    openModal({
      title: "ごみ箱移動結果",
      body,
      wide: true,
      actions: [
        { label: "操作ログを開く", className: "btn", onClick: () => api("OpenLog").catch((e) => toast(e.message, true)) },
        { label: "閉じる", className: "btn primary", onClick: closeModal },
      ],
    });
  }

  // ---------- multi-drive-copy transfer ----------

  async function openTransferReview() {
    if (state.busy) {
      toast("処理中は転送を開始できません", true);
      return;
    }
    transferBtn.disabled = true;
    try {
      const review = await api("GetTransferReview");
      if (!review || !Array.isArray(review.candidates) || review.candidates.length === 0) {
        toast("転送できるZIPまたはフォルダがありません");
        return;
      }
      showTransferReview(review);
    } catch (error) {
      toast("転送候補の取得に失敗しました: " + error.message, true);
    } finally {
      transferBtn.disabled = state.busy;
    }
  }

  function transferGroups(review) {
    const candidates = review.candidates || [];
    return {
      recommended: candidates.filter((candidate) => !candidate.review),
      review: candidates.filter((candidate) => candidate.review),
    };
  }

  function showTransferReview(review) {
    state.transferReview = review;
    state.transferCandidates = review.candidates || [];
    const groups = transferGroups(review);
    state.transferRecommendedChecked = groups.recommended.some((candidate) => candidate.selected && candidate.exists);
    state.transferReviewSelected = new Set(
      groups.review.filter((candidate) => candidate.selected && candidate.exists).map((candidate) => candidate.id)
    );
    openModal({
      title: "残ったデータをmulti-drive-copyへ引き渡す",
      wide: true,
      body: transferReviewBody(review),
      actions: [
        { label: "全選択", id: "transferSelectAllBtn", className: "btn", onClick: transferSelectAll },
        { label: "全解除", id: "transferClearBtn", className: "btn", onClick: transferClear },
        {
          label: "multi-drive-copyを起動",
          id: "transferLaunchBtn",
          className: "btn primary",
          onClick: launchTransfer,
        },
        { label: "閉じる", className: "btn", onClick: closeModal },
      ],
    });
    updateTransferSelection();
  }

  function transferReviewBody(review) {
    const groups = transferGroups(review);
    const recommendedExisting = groups.recommended.filter((candidate) => candidate.exists);
    let html =
      '<div class="stat-row">' +
      '<div class="stat"><div class="stat-label">転送候補</div><div class="stat-value">' + groups.recommended.length + groups.review.length + "</div></div>" +
      '<div class="stat"><div class="stat-label">推奨（ZIP優先）</div><div class="stat-value result-ok">' + recommendedExisting.length + "</div></div>" +
      '<div class="stat"><div class="stat-label">要確認</div><div class="stat-value">' + groups.review.length + "</div></div>" +
      '<div class="stat"><div class="stat-label">パス消失</div><div class="stat-value result-fail">' + review.missingCount + "</div></div>" +
      "</div>" +
      '<p class="muted">ZIPが存在して内容一致する組はZIPだけを推奨します。フォルダしかない場合はフォルダを推奨します。不一致・未検証・曖昧な組は要確認として表示し、既定では選択しません。</p>' +
      '<p class="muted">コピー先・プリセット・コピー後の全転送先検証・元データのごみ箱移動は multi-drive-copy の画面で設定・確認してください。</p>';
    if (!review.scriptAvailable) {
      html +=
        '<p class="modal-note">multi-drive-copy のスクリプトが見つかりません: ' +
        esc(review.scriptPath || "未設定") +
        "<br>設定ファイルの <code>multiDriveCopy.scriptPath</code> を確認してください。</p>";
    }
    if (review.busy) {
      html += '<p class="modal-note">スキャンまたはごみ箱移動の処理中です。完了後に再度開いてください。</p>';
    }
    if (groups.recommended.length) {
      html +=
        '<label class="transfer-group"><input type="checkbox" id="transferRecommended" ' +
        (state.transferRecommendedChecked ? "checked" : "") +
        (recommendedExisting.length ? "" : " disabled") +
        "> 推奨 " +
        recommendedExisting.length +
        " 件（ZIP優先）を転送対象にする</label>" +
        '<div class="transfer-preview">' +
        esc(groups.recommended.map((candidate) => candidate.path).join("\n")) +
        "</div>";
    }
    html += '<div class="detail-title" style="margin-top:14px">要確認（明示選択）</div>';
    if (groups.review.length) {
      html +=
        '<div class="transfer-review-list">' +
        groups.review
          .map((candidate) => {
            const checked = state.transferReviewSelected.has(candidate.id);
            return (
              '<label class="transfer-review-item' +
              (candidate.exists ? "" : " missing") +
              '"><input type="checkbox" data-transfer-review="' +
              esc(candidate.id) +
              '" ' +
              (checked ? "checked" : "") +
              (candidate.exists ? "" : " disabled") +
              "><span class=\"badge v-" +
              esc(candidate.verdict) +
              '">' +
              esc(VERDICT_LABEL[candidate.verdict] || candidate.verdict) +
              '</span><span class="transfer-path">' +
              esc(candidate.path) +
              '</span><span class="transfer-reason">' +
              esc(candidate.reason) +
              "</span>" +
              (candidate.exists ? "" : '<span class="result-fail">パス消失</span>') +
              "</label>"
            );
          })
          .join("") +
        "</div>";
    } else {
      html += '<p class="muted">要確認の項目はありません。</p>';
    }
    html += '<div id="transferSelectionSummary" class="transfer-selection-summary"></div>';
    return html;
  }

  function updateTransferSelection() {
    const review = state.transferReview;
    if (!review) return;
    const groups = transferGroups(review);
    const recommendedBox = document.getElementById("transferRecommended");
    state.transferRecommendedChecked = !!(recommendedBox && recommendedBox.checked);
    const seen = new Set();
    const paths = [];
    let selectedRecommended = 0;
    let selectedReview = 0;
    const add = (candidate) => {
      if (!candidate.exists) return;
      const key = candidate.path.toLowerCase();
      if (seen.has(key)) return;
      seen.add(key);
      paths.push(candidate.path);
    };
    if (state.transferRecommendedChecked) {
      groups.recommended.forEach((candidate) => {
        if (candidate.exists) {
          add(candidate);
          selectedRecommended++;
        }
      });
    }
    groups.review.forEach((candidate) => {
      if (state.transferReviewSelected.has(candidate.id) && candidate.exists) {
        add(candidate);
        selectedReview++;
      }
    });
    state.transferPaths = paths;
    const summary = document.getElementById("transferSelectionSummary");
    if (summary) {
      summary.textContent =
        "転送対象: " +
        paths.length +
        " 件（推奨 " +
        selectedRecommended +
        " / 要確認 " +
        selectedReview +
        "）";
    }
    const launch = document.getElementById("transferLaunchBtn");
    if (launch) {
      launch.disabled = paths.length === 0 || review.busy || !review.scriptAvailable;
    }
  }

  function transferSelectAll() {
    const review = state.transferReview;
    if (!review) return;
    const recommendedBox = document.getElementById("transferRecommended");
    if (recommendedBox && !recommendedBox.disabled) recommendedBox.checked = true;
    state.transferRecommendedChecked = !!(recommendedBox && recommendedBox.checked);
    state.transferReviewSelected = new Set(
      transferGroups(review).review.filter((candidate) => candidate.exists).map((candidate) => candidate.id)
    );
    modalBody.querySelectorAll("[data-transfer-review]").forEach((box) => {
      if (!box.disabled) box.checked = true;
    });
    updateTransferSelection();
  }

  function transferClear() {
    const recommendedBox = document.getElementById("transferRecommended");
    if (recommendedBox) recommendedBox.checked = false;
    state.transferRecommendedChecked = false;
    state.transferReviewSelected = new Set();
    modalBody.querySelectorAll("[data-transfer-review]").forEach((box) => {
      if (!box.disabled) box.checked = false;
    });
    updateTransferSelection();
  }

  async function launchTransfer() {
    const review = state.transferReview;
    if (!review) return;
    if (!review.scriptAvailable) {
      toast("multi-drive-copy のスクリプトが見つかりません", true);
      return;
    }
    const paths = state.transferPaths.slice();
    if (paths.length === 0) {
      toast("転送対象が選択されていません", true);
      return;
    }
    const launch = document.getElementById("transferLaunchBtn");
    if (launch) launch.disabled = true;
    state.transferLaunched = false;
    updateWorkflow();
    try {
      const result = await api("LaunchMultiDriveCopy", paths);
      state.transferLaunched = true;
      updateWorkflow();
      closeModal();
      const mode = result && result.mode === "manifest" ? "（大量パスのため -SourceListFile 経由）" : "";
      if (result && result.warning) {
        toast(result.warning + " コピー結果は本アプリでは判定しません。", true);
      } else {
        toast(
          "multi-drive-copy を起動しました" +
            mode +
            "。コピーは multi-drive-copy の画面で開始・確認してください。結果は本アプリでは判定しません。"
        );
      }
      setStatus("multi-drive-copy を起動しました。転送結果は受信側で確認してください");
    } catch (error) {
      state.transferLaunched = false;
      updateWorkflow();
      toast("multi-drive-copy を起動できませんでした: " + error.message, true);
      if (launch) launch.disabled = false;
    }
  }

  // ---------- modal ----------

  function openModal(options) {
    modalTitle.textContent = options.title || "";
    modalCard.classList.toggle("wide", !!options.wide);
    modalBody.innerHTML = options.body || "";
    modalActions.innerHTML = "";
    (options.actions || []).forEach((action) => {
      const button = document.createElement("button");
      button.className = action.className || "btn";
      button.textContent = action.label;
      if (action.id) button.id = action.id;
      if (action.disabled) button.disabled = true;
      button.addEventListener("click", action.onClick);
      modalActions.appendChild(button);
    });
    modalClose.classList.toggle("hidden", options.closable === false);
    modal.classList.remove("hidden");
  }

  function closeModal() {
    if (state.deleting) return;
    modal.classList.add("hidden");
  }

  // ---------- busy ----------

  function updateBusyUI() {
    scanBtn.disabled = state.busy || state.roots.length === 0;
    stopBtn.disabled = !state.scanning;
    browseBtn.disabled = state.busy;
    addRootBtn.disabled = state.busy;
    rootInput.disabled = state.busy;
    selectAllBtn.disabled = state.busy;
    transferBtn.disabled = state.busy;
    deleteBtn.disabled = state.busy || state.selected.size === 0;
    clearSelectionBtn.disabled = state.selected.size === 0;
    rootList.querySelectorAll("button").forEach((button) => {
      button.disabled = state.busy;
    });
  }

  function updateSortIndicators() {
    tableHeader.querySelectorAll("[data-sort]").forEach((button) => {
      const arrow = button.querySelector(".arrow");
      if (button.dataset.sort === state.sort) {
        arrow.textContent = state.desc ? "▼" : "▲";
      } else {
        arrow.textContent = "";
      }
    });
  }

  // ---------- events ----------

  function bindRuntimeEvents() {
    if (!window.runtime || typeof window.runtime.EventsOn !== "function") return;
    window.runtime.EventsOn("scan:progress", (progress) => {
      state.scanning = true;
      renderProgress(progress || {});
      updateBusyUI();
    });
    window.runtime.EventsOn("scan:done", (info) => {
      state.scanning = false;
      updateBusyUI();
      if (info && info.summary) {
        state.summary = info.summary;
        renderSummary();
      }
      hideProgressSoon();
    });
    window.runtime.EventsOn("scan:cancelled", () => {
      state.scanning = false;
      updateBusyUI();
      setStatus("スキャンを中断しました");
      hideProgressSoon();
    });
    window.runtime.EventsOn("scan:error", (message) => {
      state.scanning = false;
      updateBusyUI();
      toast("スキャンに失敗しました: " + message, true);
      progressPanel.classList.add("hidden");
    });
    window.runtime.EventsOn("delete:progress", (progress) => {
      if (!state.deleting) return;
      state.deleteTotal = Number(progress.total) || state.deleteTotal;
      modalBody.innerHTML = deleteProgressBody(progress || {});
    });
    window.runtime.EventsOn("delete:done", (response) => {
      if (response && response.summary) {
        state.summary = response.summary;
        renderSummary();
      }
    });
    window.runtime.EventsOn("delete:log_error", (message) => {
      toast("操作ログの保存に失敗しました: " + message, true);
    });
  }

  function bindEvents() {
    addRootBtn.addEventListener("click", () => addRoot(rootInput.value));
    rootInput.addEventListener("keydown", (event) => {
      if (event.key === "Enter") addRoot(rootInput.value);
    });
    browseBtn.addEventListener("click", async () => {
      try {
        const path = await api("PickFolder");
        if (path) addRoot(path);
      } catch (error) {
        toast("フォルダ選択に失敗しました: " + error.message, true);
      }
    });
    scanBtn.addEventListener("click", startScan);
    stopBtn.addEventListener("click", () => {
      api("CancelScan").catch((error) => toast(error.message, true));
      setStatus("スキャンの中断を要求しました...");
    });
    rootList.addEventListener("click", (event) => {
      const button = event.target.closest("[data-remove-root]");
      if (!button || state.busy) return;
      const index = Number(button.dataset.removeRoot);
      state.roots.splice(index, 1);
      renderRoots();
      updateBusyUI();
    });

    filterBar.addEventListener("click", (event) => {
      const button = event.target.closest("[data-filter]");
      if (!button) return;
      state.filter = button.dataset.filter;
      filterBar.querySelectorAll("[data-filter]").forEach((item) => {
        item.classList.toggle("active", item === button);
      });
      reloadRows(true);
    });

    searchInput.addEventListener("input", () => {
      if (searchTimer) clearTimeout(searchTimer);
      searchTimer = setTimeout(() => {
        state.search = searchInput.value.trim();
        reloadRows(true);
      }, 220);
    });

    tableHeader.addEventListener("click", (event) => {
      if (event.target.closest(".col-resize") || suppressSortClick) return;
      const button = event.target.closest("[data-sort]");
      if (!button) return;
      const key = button.dataset.sort;
      if (state.sort === key) {
        state.desc = !state.desc;
      } else {
        state.sort = key;
        state.desc = false;
      }
      updateSortIndicators();
      reloadRows(true);
    });

    tableScroll.addEventListener("scroll", () => {
      hideContextMenu();
      scheduleRender();
    });
    window.addEventListener("resize", scheduleRender);

    rowsEl.addEventListener("click", (event) => {
      const rowElement = event.target.closest(".row");
      if (!rowElement) return;
      const index = Number(rowElement.dataset.index);
      const row = getRow(index);
      if (!row) return;
      if (event.target.matches('input[type="checkbox"]')) {
        if (event.shiftKey) {
          event.preventDefault();
          rangeSelect(index);
        }
        return;
      }
      if (window.getSelection && window.getSelection().toString()) return;
      showDetail(row.id);
    });

    rowsEl.addEventListener("change", (event) => {
      if (!event.target.matches('input[type="checkbox"]')) return;
      const id = event.target.dataset.id;
      const rowElement = event.target.closest(".row");
      const index = rowElement ? Number(rowElement.dataset.index) : null;
      if (!id) return;
      if (event.target.checked) {
        const row = index == null ? null : getRow(index);
        if (row) addSelection(row);
        state.lastRowIndex = index;
      } else {
        removeSelection(id);
      }
      updateSelectionUI();
      renderRows();
    });

    rowsEl.addEventListener("dblclick", (event) => {
      if (event.target.matches('input[type="checkbox"]')) return;
      const rowElement = event.target.closest(".row");
      if (!rowElement) return;
      const row = getRow(Number(rowElement.dataset.index));
      if (row) api("RevealItem", row.id).catch((error) => toast(error.message, true));
    });

    rowsEl.addEventListener("contextmenu", (event) => {
      const rowElement = event.target.closest(".row");
      if (!rowElement) return;
      const row = getRow(Number(rowElement.dataset.index));
      if (!row) return;
      event.preventDefault();
      showRowContextMenu(event, row);
    });

    contextMenu.addEventListener("click", (event) => {
      const copyButton = event.target.closest("[data-copy-field]");
      if (copyButton) {
        const row = state.contextRow;
        if (row) copyText(row[copyButton.dataset.copyField]);
        hideContextMenu();
        return;
      }
      const actionButton = event.target.closest("[data-menu-action]");
      if (actionButton && actionButton.dataset.menuAction === "reveal") {
        const row = state.contextRow;
        hideContextMenu();
        if (row) api("RevealItem", row.id).catch((error) => toast(error.message, true));
      }
    });

    detailPane.addEventListener("click", (event) => {
      const copyButton = event.target.closest('[data-action="copy"]');
      if (copyButton) {
        const item = state.detailItem;
        if (item) copyText(item[copyButton.dataset.field]);
        return;
      }
      const button = event.target.closest('[data-action="reveal"]');
      if (!button) return;
      api("RevealItem", button.dataset.id).catch((error) => toast(error.message, true));
    });

    selectAllBtn.addEventListener("click", selectAllCandidates);
    clearSelectionBtn.addEventListener("click", () => {
      state.selected.clear();
      state.lastRowIndex = null;
      updateSelectionUI();
      renderRows();
    });
    resetColumnsBtn.addEventListener("click", resetColumnWidths);
    transferBtn.addEventListener("click", openTransferReview);
    deleteBtn.addEventListener("click", beginDelete);
    warningBadge.addEventListener("click", showWarnings);
    modalClose.addEventListener("click", closeModal);
    modalBody.addEventListener("change", (event) => {
      if (event.target.id === "transferRecommended") {
        state.transferRecommendedChecked = !!event.target.checked;
        updateTransferSelection();
        return;
      }
      if (event.target.matches("[data-transfer-review]")) {
        const id = event.target.dataset.transferReview;
        if (!id) return;
        if (event.target.checked) state.transferReviewSelected.add(id);
        else state.transferReviewSelected.delete(id);
        updateTransferSelection();
      }
    });
    modal.addEventListener("click", (event) => {
      if (event.target === modal) closeModal();
    });

    el("openLogBtn").addEventListener("click", () => {
      api("OpenLog").catch((error) => toast("操作ログを開けません: " + error.message, true));
    });
    settingsBtn.addEventListener("click", showSettings);

    document.addEventListener("click", (event) => {
      if (!contextMenu.contains(event.target)) hideContextMenu();
    });

    document.addEventListener("keydown", (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        event.preventDefault();
        searchInput.focus();
        searchInput.select();
      } else if (event.key === "Escape") {
        if (!contextMenu.classList.contains("hidden")) {
          hideContextMenu();
          return;
        }
        if (!modal.classList.contains("hidden")) closeModal();
      }
    });
  }

  async function waitForAPI(timeoutMs) {
    const started = Date.now();
    while (Date.now() - started < timeoutMs) {
      if (window.go && window.go.main && window.go.main.App) return true;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    return false;
  }

  async function init() {
    loadColumnWidths();
    applyColumnWidths();
    setupColumnResizers();
    bindEvents();
    const ready = await waitForAPI(6000);
    bindRuntimeEvents();
    renderRoots();
    renderSummary();
    updateSelectionUI();
    updateSortIndicators();
    updateEmptyState();
    renderRows();
    if (!ready) {
      setStatus("Wails API を待機しています");
      updateEmptyState();
      return;
    }
    try {
      const info = await api("GetState");
      if (info) {
        state.summary = info.summary || null;
        state.roots = info.roots || [];
        state.warnings = info.warnings || [];
        state.warningTotal = info.warningTotal || state.warnings.length;
        state.logPath = info.logPath || "";
        state.configPath = info.configPath || "";
        state.configWarning = info.configWarning || "";
        state.limits = info.limits || null;
        state.multiDriveCopyScript = info.multiDriveCopyScript || "";
        state.multiDriveCopyPreset = info.multiDriveCopyPreset || "";
        state.multiDriveCopyAvailable = !!info.multiDriveCopyAvailable;
        if (info.version) versionText.textContent = "v" + info.version;
        if (info.configWarning) toast(info.configWarning, true);
        renderRoots();
        renderSummary();
        renderWarnings();
        updateBusyUI();
      }
      reloadRows(true);
    } catch (error) {
      versionText.textContent = "";
      setStatus("Wails API を待機しています");
      updateEmptyState();
    }
  }

  init();
})();
