(() => {
  "use strict";

  let storage;
  try { storage = window.localStorage; } catch (_) { storage = null; }
  const sizing = window.LayoutSizing.createSizing(storage && {
    getItem: key => storage.getItem("archive-extract-cleaner.layout.v1." + key),
    setItem: (key, value) => storage.setItem("archive-extract-cleaner.layout.v1." + key, value),
  });
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
    workers: 0,
    ai: null,
    suggestions: null,
    lastCache: null,
    cacheInfo: null,
    archiveInfo: null,
    dashboard: null,
    dashboardCollapsed: false,
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
    moveOptions: [],
    moveMethods: new Map(),
    moveRequests: [],
    movePreview: null,
    detailToken: 0,
    previewToken: 0,
    preview: null,
    previewSearch: "",
    previewExpanded: new Set(),
    previewScrollTop: 0,
    previewVisible: [],
    previewVisibleDirty: true,
    dragSelect: null,
    pendingDragSelection: null,
    suppressRowClickUntil: 0,
    suppressCheckboxChangeUntil: 0,
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

  function fmtPreviewDate(value) {
    const text = fmtDate(value);
    return text === "—" ? text : text.replace(/:\d{2}$/, "");
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

  function setupLayoutSizing() {
    const upper = el("upperRegion");
    const splitter = el("upperSplitter");
    const slider = el("rowHeight");
    const output = el("rowHeightValue");
    const app = el("app");
    const available = () => Math.max(0, app.clientHeight - document.querySelector(".topbar").offsetHeight - document.querySelector(".statusbar").offsetHeight - splitter.offsetHeight);
    function applyUpper() {
      const max = Math.min(800, Math.max(0, available() - 120));
      const height = sizing.upper(available());
      upper.style.height = height + "px";
      splitter.setAttribute("aria-valuemin", String(Math.min(80, max)));
      splitter.setAttribute("aria-valuemax", String(max));
      splitter.setAttribute("aria-valuenow", String(height));
      scheduleRender();
    }
    function applyRow() {
      const height = sizing.row();
      document.documentElement.style.setProperty("--row-h", height + "px");
      slider.value = String(height);
      output.textContent = height + "px";
      renderRows();
    }
    slider.addEventListener("input", () => { sizing.setRow(slider.value); applyRow(); });
    splitter.addEventListener("keydown", event => {
      const current = sizing.upper(available());
      let next;
      if (event.key === "ArrowUp") next = current - (event.shiftKey ? 40 : 10);
      else if (event.key === "ArrowDown") next = current + (event.shiftKey ? 40 : 10);
      else if (event.key === "Home") next = 80;
      else if (event.key === "End") next = Math.min(800, available() - 120);
      else return;
      event.preventDefault();
      sizing.setUpper(Math.min(next, Math.max(80, available() - 120)));
      applyUpper();
    });
    splitter.addEventListener("pointerdown", event => {
      if (event.button !== 0) return;
      event.preventDefault();
      const start = sizing.upper(available());
      const y = event.clientY;
      const pointerId = event.pointerId;
      try { splitter.setPointerCapture(pointerId); } catch (_) { /* window listeners suffice */ }
      const move = e => {
        if (e.pointerId !== pointerId) return;
        sizing.setUpper(Math.min(start + e.clientY - y, Math.max(80, available() - 120)));
        applyUpper();
      };
      const stop = e => {
        if (e.pointerId !== pointerId) return;
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", stop);
        window.removeEventListener("pointercancel", stop);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", stop);
      window.addEventListener("pointercancel", stop);
    });
    window.addEventListener("resize", applyUpper);
    applyUpper();
    applyRow();
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
    DELETE_CANDIDATE_IGNORABLE: "補助差分のみ",
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

  const MOVE_METHOD_LABEL = {
    folder: "解凍済みフォルダのみ",
    zip: "ZIPのみ",
    both: "ZIPとフォルダの両方",
  };

  const PREVIEW_KIND_LABEL = {
    file: "ファイル",
    directory: "フォルダ",
    link: "リンク",
    error: "エラー",
    other: "その他",
  };

  function verdictBadge(item) {
    const verdict = item.verdict || item.Verdict || "";
    let html = '<span class="badge v-' + esc(verdict) + '">' + esc(VERDICT_LABEL[verdict] || verdict) + "</span>";
    const status = item.status || item.Status || "";
    const zipMoved = !!item.zipMoved;
    const folderMoved = !!item.folderMoved;
    if (zipMoved && folderMoved) {
      html += '<span class="badge badge-deleted">ごみ箱へ移動済み</span>';
    } else if (zipMoved) {
      html += '<span class="badge badge-deleted">ZIP移動済み</span>';
    } else if (folderMoved) {
      html += '<span class="badge badge-deleted">フォルダ移動済み</span>';
    } else if (status === "deleted") {
      html += '<span class="badge badge-deleted">ごみ箱へ移動済み</span>';
    }
    if (status === "error") {
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
    virtualEl.style.height = Math.max(0, total) * sizing.row() + "px";

    if (!total) {
      rowsEl.innerHTML = "";
      emptyState.classList.remove("hidden");
      updateEmptyState();
      return;
    }
    emptyState.classList.add("hidden");

    const scrollTop = tableScroll.scrollTop;
    const viewport = tableScroll.clientHeight;
    const start = Math.max(0, Math.floor(scrollTop / sizing.row()) - OVERSCAN);
    const visibleCount = Math.ceil(viewport / sizing.row()) + OVERSCAN * 2;
    const end = Math.min(total, start + visibleCount);

    rowsEl.style.transform = "translate3d(0," + start * sizing.row() + "px,0)";

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
      (row.nameVariant ? ' <span class="badge badge-variant" title="ZIP名とフォルダ名が異なります（内容一致）">名前差</span>' : "") +
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
        applyPendingDragSelection();
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
    if (page.dashboard) {
      state.dashboard = page.dashboard;
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
    cancelDragSelection();
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

  const DRAG_EDGE = 28;
  const DRAG_MAX_SCROLL = 20;

  function applyDragRow(row, mode) {
    const selected = state.selected.has(row.id);
    if (mode === "add") {
      if (selected) return false;
      addSelection(row);
      return true;
    }
    if (!selected) return false;
    removeSelection(row.id);
    return true;
  }

  function dragIndexAtPoint(x, y) {
    if (!state.total) return null;
    const rect = tableScroll.getBoundingClientRect();
    if (x < rect.left || x > rect.right || y < rect.top || y > rect.bottom) return null;
    if (typeof document.elementFromPoint === "function") {
      try {
        const hit = document.elementFromPoint(x, y);
        const rowElement = hit && hit.closest ? hit.closest(".row") : null;
        if (rowElement && rowsEl.contains(rowElement)) {
          const index = Number(rowElement.dataset.index);
          if (Number.isInteger(index) && index >= 0) return index;
        }
      } catch (_) {
        // フォールバックして座標から行を求める
      }
    }
    const headerHeight = tableHeader.getBoundingClientRect().height || 34;
    const contentY = y - rect.top - headerHeight + tableScroll.scrollTop;
    const index = Math.floor(contentY / sizing.row());
    if (!Number.isFinite(index)) return null;
    return Math.max(0, Math.min(state.total - 1, index));
  }

  function applyDragIndex(index) {
    const drag = state.dragSelect;
    if (!drag || index == null || state.total <= 0) return;
    const target = Math.max(0, Math.min(state.total - 1, Number(index)));
    if (!Number.isFinite(target)) return;
    const from = Math.min(drag.lastIndex, target);
    const to = Math.max(drag.lastIndex, target);
    drag.lastIndex = target;
    let changed = false;
    let firstPending = Infinity;
    let lastPending = -1;
    for (let i = from; i <= to; i++) {
      if (drag.visited.has(i)) continue;
      const row = getRow(i);
      if (!row) {
        drag.pending.add(i);
        firstPending = Math.min(firstPending, i);
        lastPending = Math.max(lastPending, i);
        continue;
      }
      drag.visited.add(i);
      if (row.deletable && applyDragRow(row, drag.mode)) changed = true;
    }
    if (lastPending >= 0) {
      ensurePages(Math.floor(firstPending / PAGE_SIZE), Math.floor(lastPending / PAGE_SIZE));
    }
    if (changed) {
      updateSelectionUI();
      scheduleRender();
    }
  }

  function applyPendingDragSelection() {
    const drag = state.dragSelect || state.pendingDragSelection;
    if (!drag || !drag.pending || drag.pending.size === 0) {
      if (!state.dragSelect) state.pendingDragSelection = null;
      return;
    }
    let changed = false;
    let remaining = false;
    for (const index of Array.from(drag.pending)) {
      const row = getRow(index);
      if (!row) {
        remaining = true;
        continue;
      }
      drag.pending.delete(index);
      if (drag.visited.has(index)) continue;
      drag.visited.add(index);
      if (row.deletable && applyDragRow(row, drag.mode)) changed = true;
    }
    if (remaining && state.total > 0) {
      for (const index of Array.from(drag.pending)) {
        if (index >= state.total) drag.pending.delete(index);
      }
    }
    if (!state.dragSelect && drag.pending.size === 0) state.pendingDragSelection = null;
    if (changed) {
      updateSelectionUI();
      scheduleRender();
    }
  }

  function clearPendingDragSelection() {
    state.pendingDragSelection = null;
  }

  function dragAutoScrollTick() {
    const drag = state.dragSelect;
    if (!drag) return;
    drag.autoScrollFrame = 0;
    const rect = tableScroll.getBoundingClientRect();
    let speed = 0;
    if (drag.clientX >= rect.left && drag.clientX <= rect.right) {
      if (drag.clientY < rect.top + DRAG_EDGE) {
        speed = -Math.ceil(((rect.top + DRAG_EDGE - drag.clientY) / DRAG_EDGE) * DRAG_MAX_SCROLL);
      } else if (drag.clientY > rect.bottom - DRAG_EDGE) {
        speed = Math.ceil(((drag.clientY - (rect.bottom - DRAG_EDGE)) / DRAG_EDGE) * DRAG_MAX_SCROLL);
      }
    }
    speed = Math.max(-DRAG_MAX_SCROLL, Math.min(DRAG_MAX_SCROLL, speed));
    if (speed !== 0) {
      const before = tableScroll.scrollTop;
      tableScroll.scrollTop += speed;
      if (tableScroll.scrollTop !== before) {
        const index = dragIndexAtPoint(drag.clientX, drag.clientY);
        if (index != null) applyDragIndex(index);
      }
    }
    drag.autoScrollFrame = requestAnimationFrame(dragAutoScrollTick);
  }

  function onDragMove(event) {
    const drag = state.dragSelect;
    if (!drag || event.pointerId !== drag.pointerId) return;
    event.preventDefault();
    drag.clientX = event.clientX;
    drag.clientY = event.clientY;
    const index = dragIndexAtPoint(drag.clientX, drag.clientY);
    if (index != null) applyDragIndex(index);
  }

  function onDragEnd(event) {
    const drag = state.dragSelect;
    if (!drag || (event && event.pointerId !== drag.pointerId)) return;
    if (event && event.type !== "pointercancel") {
      drag.clientX = event.clientX;
      drag.clientY = event.clientY;
      const index = dragIndexAtPoint(drag.clientX, drag.clientY);
      if (index != null) applyDragIndex(index);
    }
    finishDragSelection(!event || event.type !== "pointercancel");
  }

  function finishDragSelection(suppressClick) {
    const drag = state.dragSelect;
    if (!drag) return;
    if (drag.autoScrollFrame) cancelAnimationFrame(drag.autoScrollFrame);
    state.dragSelect = null;
    document.body.classList.remove("drag-selecting");
    window.removeEventListener("pointermove", onDragMove);
    window.removeEventListener("pointerup", onDragEnd);
    window.removeEventListener("pointercancel", onDragEnd);
    if (typeof rowsEl.releasePointerCapture === "function" && drag.pointerId != null) {
      try {
        if (!rowsEl.hasPointerCapture || rowsEl.hasPointerCapture(drag.pointerId)) {
          rowsEl.releasePointerCapture(drag.pointerId);
        }
      } catch (_) {
        // ポインターキャプチャ未対応・解放済みは無視
      }
    }
    if (suppressClick !== false) {
      state.suppressRowClickUntil = performance.now() + 350;
      state.suppressCheckboxChangeUntil = performance.now() + 350;
    } else {
      state.suppressRowClickUntil = 0;
      state.suppressCheckboxChangeUntil = 0;
    }
    state.lastRowIndex = drag.lastIndex;
    if (suppressClick !== false && drag.pending.size > 0) {
      state.pendingDragSelection = {
        mode: drag.mode,
        visited: drag.visited,
        pending: drag.pending,
      };
      let firstPending = Infinity;
      let lastPending = -1;
      drag.pending.forEach((index) => {
        firstPending = Math.min(firstPending, index);
        lastPending = Math.max(lastPending, index);
      });
      if (lastPending >= 0) {
        ensurePages(Math.floor(firstPending / PAGE_SIZE), Math.floor(lastPending / PAGE_SIZE));
      }
    } else {
      state.pendingDragSelection = null;
      drag.pending.clear();
    }
    updateSelectionUI();
    scheduleRender();
  }

  function cancelDragSelection() {
    clearPendingDragSelection();
    if (!state.dragSelect) return;
    finishDragSelection(false);
  }

  function startDragSelection(event) {
    if (state.dragSelect || state.busy) return;
    if (event.button !== 0 || event.shiftKey) return;
    if (event.pointerType === "touch") return;
    const checkbox = event.target.closest ? event.target.closest('input[type="checkbox"][data-role="select"]') : null;
    if (!checkbox) return;
    const rowElement = checkbox.closest(".row");
    if (!rowElement) return;
    const index = Number(rowElement.dataset.index);
    const row = getRow(index);
    if (!row || !row.deletable) return;
    event.preventDefault();
    state.pendingDragSelection = null;
    if (typeof rowsEl.setPointerCapture === "function" && event.pointerId != null) {
      try {
        rowsEl.setPointerCapture(event.pointerId);
      } catch (_) {
        // ポインターキャプチャ未対応時はwindowイベントで追跡する
      }
    }
    state.dragSelect = {
      pointerId: event.pointerId,
      mode: checkbox.checked ? "remove" : "add",
      startIndex: index,
      lastIndex: index,
      clientX: event.clientX,
      clientY: event.clientY,
      autoScrollFrame: 0,
      visited: new Set(),
      pending: new Set(),
    };
    state.lastRowIndex = index;
    document.body.classList.add("drag-selecting");
    window.addEventListener("pointermove", onDragMove);
    window.addEventListener("pointerup", onDragEnd);
    window.addEventListener("pointercancel", onDragEnd);
    applyDragIndex(index);
    state.dragSelect.autoScrollFrame = requestAnimationFrame(dragAutoScrollTick);
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
    clearPendingDragSelection();
    selectAllBtn.disabled = true;
    try {
      const ids = await api("CandidateIDs", currentQuery(0, 0));
      if (!ids || ids.length === 0) {
        toast("現在の条件で選択できる削除候補がありません");
        return;
      }
      const options = await api("GetMoveOptions", ids);
      state.selected.clear();
      (options || [])
        .filter((option) => option.defaultMethod)
        .forEach((option) => {
          const size = option.defaultMethod === "zip" ? option.zipSize : option.folderSize;
          state.selected.set(option.id, {
            id: option.id,
            name: option.name,
            size,
            zipPath: option.zipPath,
            folderPath: option.folderPath,
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
      renderDashboard();
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
    const ignorable = byVerdict.DELETE_CANDIDATE_IGNORABLE || 0;
    if (ignorable) {
      chips.splice(2, 0, '<span class="chip">補助差分のみ <strong>' + ignorable + "</strong></span>");
    }
    if (summary.deletedCount) {
      chips.push('<span class="chip">移動済み <strong>' + summary.deletedCount + "</strong></span>");
    }
    if (summary.durationMs) {
      chips.push('<span class="chip">所要時間 <strong>' + esc(fmtDuration(summary.durationMs)) + "</strong></span>");
    }
    summaryChips.innerHTML = chips.join("");
    renderDashboard();
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

  // cacheStatusText は設定ダイアログ向けのキャッシュ状態を返す。
  function cacheStatusText() {
    const cache = state.cacheInfo;
    if (!cache || !cache.enabled) return "無効";
    return "安全確認のため再利用を一時停止中";
  }

  // ---------- dashboard ----------

  // renderDashboard は容量の内訳を描画する。
  // スキャン全体の値であり、一覧の絞り込みとは独立させる。
  function renderDashboard() {
    const panel = el("dashboardPanel");
    const body = el("dashboardBody");
    const scope = el("dashboardScope");
    if (!panel || !body) return;
    const dash = state.dashboard;
    if (!dash || !dash.scan || !dash.scan.total || !dash.scan.total.count) {
      panel.classList.add("hidden");
      return;
    }
    panel.classList.remove("hidden");

    const scan = dash.scan;
    const moves = dash.moves || {};
    const cards = [];

    cards.push(
      '<div class="dash-card reclaim"><span class="dash-label">回収見込み</span>' +
        '<div class="dash-value">' + esc(fmtBytes(scan.reclaimable.bytes || 0)) + "</div>" +
        '<div class="dash-sub">削除候補 ' + (scan.reclaimable.count || 0) + " 件（内容一致済みのみ）</div></div>"
    );
    cards.push(
      '<div class="dash-card"><span class="dash-label">ZIPを保持</span>' +
        '<div class="dash-value">' + esc(fmtBytes(scan.keepArchive.bytes || 0)) + "</div>" +
        '<div class="dash-sub">' + (scan.keepArchive.count || 0) + " 件</div></div>"
    );
    cards.push(
      '<div class="dash-card"><span class="dash-label">フォルダを保持</span>' +
        '<div class="dash-value">' + esc(fmtBytes(scan.keepFolder.bytes || 0)) + "</div>" +
        '<div class="dash-sub">' + (scan.keepFolder.count || 0) + " 件</div></div>"
    );
    const excluded = (scan.mismatch.count || 0) + (scan.ambiguous.count || 0) + (scan.unverified.count || 0);
    const excludedBytes = (scan.mismatch.bytes || 0) + (scan.ambiguous.bytes || 0) + (scan.unverified.bytes || 0);
    cards.push(
      '<div class="dash-card excluded"><span class="dash-label">削減対象外</span>' +
        '<div class="dash-value">' + esc(fmtBytes(excludedBytes)) + "</div>" +
        '<div class="dash-sub">不一致 ' + (scan.mismatch.count || 0) +
        " / 曖昧 " + (scan.ambiguous.count || 0) +
        " / 未検証 " + (scan.unverified.count || 0) + " 件</div></div>"
    );

    const moved = Number(moves.movedBytes) || 0;
    const moveCount = (moves.succeeded && moves.succeeded.count) || 0;
    if (moveCount > 0 || moved > 0) {
      cards.push(
        '<div class="dash-card"><span class="dash-label">ごみ箱へ移動した容量</span>' +
          '<div class="dash-value">' + esc(fmtBytes(moved)) + "</div>" +
          '<div class="dash-sub">成功 ' + moveCount + " 件" +
          ((moves.partial && moves.partial.count) ? " / 部分成功 " + moves.partial.count + " 件" : "") +
          ((moves.failed && moves.failed.count) ? " / 失敗 " + moves.failed.count + " 件" : "") +
          "</div></div>"
      );
    }

    cards.push(
      '<div class="dash-note">' +
        "「回収見込み」は内容一致を確認済みの削除候補だけです。不一致・未検証は含めません。" +
        "ごみ箱へ移動した容量は、ごみ箱を空にするまでディスクの空き容量として戻りません。" +
        "</div>"
    );

    body.innerHTML = cards.join("");

    if (dash.filtered) {
      scope.textContent = "スキャン全体の値（一覧は " + (dash.filteredCount || 0) + " 件に絞り込み中）";
    } else {
      scope.textContent = "スキャン全体の値";
    }
  }

  // archiveStatusText はアーカイブ対応状況を1行にまとめる。
  function archiveStatusText() {
    const info = state.archiveInfo;
    if (!info) return "ZIP";
    const formats = Array.isArray(info.formats) ? info.formats.join(" / ") : ".zip";
    if (!info.toolAvailable) {
      return formats + "（7z / RAR は 7-Zip 未検出のため検証できません）";
    }
    return formats + "（7-Zip: " + (info.toolPath || "検出済み") + "）";
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
      "<dt>照合の並列度</dt><dd>" + esc(state.workers ? state.workers + "（設定値）" : "自動") + "</dd>" +
      "<dt>スキャンキャッシュ</dt><dd>" + esc(cacheStatusText()) + "</dd>" +
      "<dt>対応アーカイブ</dt><dd>" + esc(archiveStatusText()) + "</dd>" +
      "<dt>AI連携</dt><dd>" +
        esc(
          !state.ai || !state.ai.enabled
            ? "無効"
            : state.ai.available
              ? "有効（" + state.ai.model + "）"
              : "設定済み・接続情報が不足"
        ) +
        "</dd>" +
      "<dt>multi-drive-copy</dt><dd>" + esc(state.multiDriveCopyScript || "—") + (state.multiDriveCopyAvailable ? "" : "（未検出）") + "</dd>" +
      "<dt>転送プリセット</dt><dd>" + esc(state.multiDriveCopyPreset || "設定画面を開く") + "</dd>" +
      "</dl>" +
      '<p class="modal-note">上限を超えたZIPやフォルダは UNVERIFIED となり、削除候補にはなりません。設定ファイルを変更した場合はアプリを再起動してください。</p>' +
      '<p class="modal-note">multi-drive-copy のスクリプトとプリセットは設定ファイルの <code>multiDriveCopy</code> で指定します。プリセット未指定時は multi-drive-copy の設定画面が開きます。</p>' +
      '<p class="modal-note">キャッシュの再利用は安全確認のため一時停止中です。通常スキャンでも内容を再照合します。</p>' +
      '<p class="modal-note">7z / RAR の検証には <a href="https://www.7-zip.org/" target="_blank" rel="noreferrer">7-Zip</a> が必要です。未インストールの環境では 7z / RAR は「未検証」として保持され、削除候補になりません。場所を指定する場合は設定ファイルの <code>archiveTool</code> に <code>7z.exe</code> のフルパスを書いてください。</p>';
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
        {
          label: "キャッシュを破棄",
          className: "btn",
          onClick: () => clearCache(),
        },
        { label: "閉じる", className: "btn primary", onClick: closeModal },
      ],
    });
  }

  async function clearCache() {
    try {
      await api("ClearCache");
      state.cacheInfo = await api("GetCacheState");
      state.lastCache = null;
      toast("スキャンキャッシュを破棄しました。次回は完全スキャンになります");
      closeModal();
      showSettings();
    } catch (error) {
      toast("キャッシュを破棄できません: " + error.message, true);
    }
  }

  // ---------- detail ----------

  async function showDetail(id) {
    state.activeId = id;
    const detailToken = ++state.detailToken;
    const previewToken = ++state.previewToken;
    state.preview = { id, source: "", sources: [], entries: [], truncated: false, loading: true, error: "" };
    state.previewSearch = "";
    state.previewExpanded = new Set();
    state.previewScrollTop = 0;
    state.previewVisible = [];
    state.previewVisibleDirty = true;
    state.detailItem = null;
    detailPane.innerHTML =
      '<div class="detail-empty"><div class="detail-title">詳細</div><p>読み込み中...</p></div>';
    renderRows();
    loadPreview(id, "", previewToken);
    try {
      const item = await api("GetItemDetail", id);
      if (detailToken !== state.detailToken) return;
      renderDetail(item);
    } catch (error) {
      if (detailToken !== state.detailToken) return;
      toast("詳細の取得に失敗しました: " + error.message, true);
    }
  }

  function renderDetail(item) {
    state.detailItem = item;
    const rows = [];
    rows.push(["判定", verdictBadge(item)]);
    if (item.status === "error") {
      rows.push(["状態", '<span class="result-fail">エラー</span>']);
    } else if (item.zipMoved && item.folderMoved) {
      rows.push(["状態", '<span class="result-ok">ZIPとフォルダをごみ箱へ移動済み</span>']);
    } else if (item.zipMoved) {
      rows.push(["状態", '<span class="result-ok">ZIPをごみ箱へ移動済み（フォルダは保持）</span>']);
    } else if (item.folderMoved) {
      rows.push(["状態", '<span class="result-ok">フォルダをごみ箱へ移動済み（ZIPは保持）</span>']);
    } else if (item.status === "deleted") {
      rows.push(["状態", '<span class="result-ok">ごみ箱へ移動済み</span>']);
    }
    rows.push(["アーカイブ", item.zipPath || "—"]);
    rows.push(["解凍済みフォルダ", item.folderPath || "—"]);
    if (item.nameVariant) {
      rows.push(["名前の対応", '<span class="badge badge-variant">名前差あり（内容一致）</span>']);
    }
    rows.push(["親パス", item.parentPath || "—"]);
    rows.push(["対象サイズ", fmtBytes(item.size)]);
    if (item.zipSize) rows.push(["アーカイブサイズ", fmtBytes(item.zipSize)]);
    if (item.folderSize) rows.push(["フォルダサイズ", fmtBytes(item.folderSize)]);
    if (item.matchMode) rows.push(["一致方式", MATCH_LABEL[item.matchMode] || item.matchMode]);
    if (item.fileCount || item.dirCount) {
      rows.push(["ファイル / ディレクトリ", (item.fileCount || 0) + " / " + (item.dirCount || 0)]);
    }
    if (item.zipEntries) rows.push(["アーカイブエントリ数", String(item.zipEntries)]);
    if (item.comparedBytes) rows.push(["比較バイト数", fmtBytes(item.comparedBytes)]);
    rows.push(["確認日時", fmtDate(item.checkedAt)]);
    if (item.deletedAt && !String(item.deletedAt).startsWith("0001-")) {
      rows.push(["移動日時", fmtDate(item.deletedAt)]);
    }
    if (item.deleteError) rows.push(["エラー", item.deleteError]);

    const copyActions = [
      ["name", "名前をコピー"],
      ["zipPath", "アーカイブのパスをコピー"],
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
    const folderDisabled = !item.folderPath || item.folderMoved;
    const zipDisabled = !item.zipPath || item.zipMoved;

    let html =
      '<div class="detail-block"><h3 class="detail-name">' +
      esc(item.name) +
      "</h3><p class=\"detail-reason\">" +
      esc(item.reason || "") +
      "</p>" +
      '<div class="detail-actions">' +
      '<button class="btn small" data-action="reveal-folder" data-id="' +
      esc(item.id) +
      '"' +
      (folderDisabled ? " disabled" : "") +
      '>解凍済みフォルダを開く</button>' +
      '<button class="btn small" data-action="reveal-zip" data-id="' +
      esc(item.id) +
      '"' +
      (zipDisabled ? " disabled" : "") +
      '>ZIPの場所を表示</button>' +
      copyButtons +
      "</div></div>" +
      '<div id="previewSection" class="preview-section"></div>' +
      '<div class="detail-block"><dl class="detail-grid">' +
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
    html += renderAISection(item);
    html +=
      '<div class="detail-note">ZIPと解凍済みフォルダのいずれか、または両方をWindowsのごみ箱へ移動できます。完全削除へのフォールバックは行いません。ZIPとフォルダの両方の移動は、最後のローカルコピーを失う可能性があります。</div>';
    detailPane.innerHTML = html;
    renderPreviewSection();
  }

  const AI_DECISION_LABEL = {
    delete_folder: "フォルダを削除してよい",
    delete_zip: "ZIPを削除してよい",
    delete_both: "両方削除してよい",
    keep: "保持すべき",
    unknown: "判断できない",
  };

  // AIは推薦のみ。削除は既存の完全一致判定を通過した項目だけに限定する。
  function renderAISection(item) {
    const stateInfo = state.ai || {};
    const suggestion = state.suggestions ? state.suggestions[item.id] : null;
    const alreadyCandidate = item.deletable || item.verdict === "DELETE_CANDIDATE" || item.verdict === "DELETE_CANDIDATE_IGNORABLE";
    const unverified = item.verdict === "UNVERIFIED";
    let body = "";
    if (alreadyCandidate) {
      body =
        '<p class="modal-note">この項目は内容の完全一致を確認済みです。AIの判断は不要です。</p>';
    } else if (unverified) {
      body =
        '<p class="modal-note">未検証の項目はAIの対象外です。内容を確認できないため保持してください。</p>';
    } else if (!stateInfo.enabled) {
      body =
        '<p class="modal-note">AI連携は無効です。設定ファイルの <code>ai.enabled</code> と環境変数 <code>' +
        esc(stateInfo.keyEnv || "ARCHIVE_CLEANER_AI_KEY") +
        "</code> を設定すると、不一致・曖昧な項目についてAIの推薦を取得できます。</p>";
    } else if (!stateInfo.available) {
      body =
        '<p class="modal-note">AIの接続情報が不足しています。環境変数 <code>' +
        esc(stateInfo.keyEnv || "ARCHIVE_CLEANER_AI_KEY") +
        "</code> にAPIキーを設定してください。</p>";
    } else if (suggestion) {
      if (suggestion.error) {
        body = '<p class="modal-note">AIの推薦を取得できませんでした: ' + esc(suggestion.error) + "</p>";
      } else {
        const percent = Math.round((Number(suggestion.confidence) || 0) * 100);
        body =
          '<dl class="detail-grid"><dt>AIの推薦</dt><dd>' +
          esc(AI_DECISION_LABEL[suggestion.decision] || suggestion.decision) +
          "</dd><dt>信頼度</dt><dd>" +
          percent +
          "%</dd><dt>モデル</dt><dd>" +
          esc(suggestion.model || "—") +
          "</dd></dl>" +
          (Array.isArray(suggestion.reasons) && suggestion.reasons.length
            ? '<ul class="detail-list">' +
              suggestion.reasons.map((reason) => "<li>" + esc(reason) + "</li>").join("") +
              "</ul>"
            : "") +
          '<p class="modal-note">AIの推薦は削除の根拠になりません。内容の完全一致を確認できていないため、この項目は削除できません。</p>';
      }
    } else {
      body =
        '<p class="modal-note">この項目は内容が一致していません。AIに推薦を求められますが、削除には使用できません。</p>' +
        '<button class="btn small" data-action="ai-suggest" data-id="' +
        esc(item.id) +
        '">AIに推薦を求める</button>';
    }
    return (
      '<div class="detail-block"><div class="detail-title">AIの判断（参考）</div>' + body + "</div>"
    );
  }

  async function exportReport(format, button) {
    if (state.busy) {
      toast("処理中は書き出せません", true);
      return;
    }
    const label = button ? button.textContent : "";
    if (button) {
      button.disabled = true;
      button.textContent = "書き出し中...";
    }
    try {
      const result = await api("ExportReport", format);
      toast(result.count + " 件を書き出しました: " + result.path);
    } catch (error) {
      toast("書き出しに失敗しました: " + error.message, true);
    } finally {
      if (button) {
        button.disabled = false;
        button.textContent = label;
      }
    }
  }

  async function requestSuggestion(id, button) {
    if (!state.suggestions) state.suggestions = {};
    if (button) {
      button.disabled = true;
      button.textContent = "問い合わせ中...";
    }
    try {
      const info = await api("SuggestForItem", id);
      state.suggestions[id] = info;
      if (state.detailItem && state.detailItem.id === id) renderDetail(state.detailItem);
      if (info && info.error) toast(info.error, true);
    } catch (error) {
      state.suggestions[id] = { id, error: error.message };
      if (state.detailItem && state.detailItem.id === id) renderDetail(state.detailItem);
      toast("AIの推薦を取得できませんでした: " + error.message, true);
    }
  }

  async function loadPreview(id, source, token) {
    const previous = state.preview || { id, sources: [] };
    state.preview = {
      id,
      source: source || previous.source || "",
      sources: previous.sources || [],
      entries: [],
      truncated: false,
      loading: true,
      error: "",
    };
    renderPreviewSection();
    try {
      const response = await api("GetPreview", id, source || "");
      if (token !== state.previewToken || state.activeId !== id) return;
      state.preview = {
        id,
        source: (response && response.source) || "",
        sources: (response && response.sources) || [],
        entries: (response && response.entries) || [],
        truncated: !!(response && response.truncated),
        loading: false,
        error: (response && response.error) || "",
      };
      state.previewSearch = "";
      state.previewExpanded = autoExpandPreview(state.preview.entries);
      state.previewScrollTop = 0;
      state.previewVisibleDirty = true;
      renderPreviewSection();
    } catch (error) {
      if (token !== state.previewToken || state.activeId !== id) return;
      state.preview = {
        ...state.preview,
        entries: [],
        loading: false,
        error: "プレビューを読み込めません: " + error.message,
      };
      renderPreviewSection();
    }
  }

  function autoExpandPreview(entries) {
    const top = (entries || []).filter((entry) => entry.depth === 0);
    if (top.length === 1 && top[0].kind === "directory") {
      return new Set([top[0].path]);
    }
    return new Set();
  }

  function previewSourceName(kind) {
    return kind === "zip" ? "アーカイブ" : "フォルダ";
  }

  function renderPreviewSection() {
    const section = document.getElementById("previewSection");
    if (!section) return;
    const preview = state.preview;
    let html = '<div class="preview-head"><div class="detail-title">中身プレビュー</div><div class="preview-tabs">';
    if (preview && preview.sources.length) {
      html += preview.sources
        .map(
          (source) =>
            '<button type="button" class="preview-tab' +
            (preview.source === source.kind ? " active" : "") +
            '" data-preview-source="' +
            esc(source.kind) +
            '"' +
            (source.available ? "" : " disabled") +
            ' title="' +
            esc(source.path) +
            '">' +
            esc(previewSourceName(source.kind)) +
            (source.available ? "" : "（利用不可）") +
            "</button>"
        )
        .join("");
    }
    html += "</div></div>";

    if (!preview) {
      section.innerHTML = html + '<p class="preview-empty">行を選択すると中身を表示します。</p>';
      return;
    }
    if (preview.error) {
      section.innerHTML = html + '<p class="preview-error">' + esc(preview.error) + "</p>";
      return;
    }
    if (preview.loading) {
      section.innerHTML = html + '<p class="preview-loading">読み込み中...</p>';
      return;
    }
    if (!preview.entries.length) {
      section.innerHTML = html + '<p class="preview-empty">項目がありません。</p>';
      return;
    }
    const note =
      preview.source === "zip"
        ? "ZIP内の相対パスです（実ファイルのフルパスではありません）。読み取りのみで解凍しません。"
        : "解凍済みフォルダを読み取り専用で表示しています。";
    html +=
      '<div class="preview-toolbar"><input id="previewSearch" class="preview-search" type="search" placeholder="名前・パス・拡張子で絞り込み" value="' +
      esc(state.previewSearch) +
      '" autocomplete="off"><span id="previewCount" class="preview-count"></span></div>' +
      '<p class="preview-source-note">' +
      esc(note) +
      "</p>" +
      '<div id="previewList" class="preview-list"><div id="previewVirtual" class="preview-virtual"></div></div>';
    section.innerHTML = html;
    const list = document.getElementById("previewList");
    if (list) list.scrollTop = state.previewScrollTop;
    renderPreviewList();
  }

  const PREVIEW_ROW_HEIGHT = 26;

  function renderPreviewList() {
    const preview = state.preview;
    const list = document.getElementById("previewList");
    const virtual = document.getElementById("previewVirtual");
    if (!preview || !list || !virtual) return;
    const query = (state.previewSearch || "").trim().toLowerCase();
    const entries = preview.entries || [];
    let visible;
    if (state.previewVisibleDirty) {
      if (query) {
        visible = entries.filter(
          (entry) =>
            String(entry.path || "").toLowerCase().includes(query) ||
            String(entry.name || "").toLowerCase().includes(query)
        );
      } else {
        visible = entries.filter((entry) => {
          if (!entry.depth) return true;
          let parent = entry.path;
          for (;;) {
            const index = parent.lastIndexOf("/");
            if (index < 0) break;
            parent = parent.slice(0, index);
            if (!state.previewExpanded.has(parent)) return false;
          }
          return true;
        });
      }
      state.previewVisible = visible;
      state.previewVisibleDirty = false;
    } else {
      visible = state.previewVisible;
    }
    const count = document.getElementById("previewCount");
    if (count) {
      count.textContent =
        (query ? visible.length + " 件一致 / " : visible.length + " / ") +
        entries.length +
        " 件" +
        (preview.truncated ? "（先頭 " + entries.length + " 件まで）" : "");
    }
    if (!visible.length) {
      virtual.style.height = "0px";
      virtual.innerHTML = '<p class="preview-empty">' + (query ? "一致する項目はありません。" : "項目がありません。") + "</p>";
      return;
    }
    const total = visible.length;
    const viewport = list.clientHeight || 260;
    const start = Math.max(0, Math.floor(state.previewScrollTop / PREVIEW_ROW_HEIGHT) - 6);
    const end = Math.min(total, Math.ceil((state.previewScrollTop + viewport) / PREVIEW_ROW_HEIGHT) + 6);
    const rows = [];
    for (let index = start; index < end; index++) {
      const entry = visible[index];
      const isDir = entry.kind === "directory";
      const expanded = state.previewExpanded.has(entry.path);
      const indent = query ? 0 : Math.max(0, Number(entry.depth) || 0) * 14;
      const toggle = isDir
        ? '<button type="button" class="preview-toggle" data-preview-toggle="' +
          esc(entry.path) +
          '" aria-label="' +
          (expanded ? "折りたたむ" : "展開") +
          '">' +
          (expanded ? "▾" : "▸") +
          "</button>"
        : '<span class="preview-toggle preview-toggle-placeholder"></span>';
      rows.push(
        '<div class="preview-row' +
          (isDir ? " is-dir" : "") +
          '" style="padding-left:' +
          indent +
          'px" title="' +
          esc(entry.path) +
          '">' +
          toggle +
          '<span class="preview-kind">' +
          esc(PREVIEW_KIND_LABEL[entry.kind] || entry.kind) +
          "</span>" +
          '<span class="preview-name">' +
          esc(query ? entry.path : entry.name) +
          "</span>" +
          '<span class="preview-size">' +
          esc(isDir ? "—" : fmtBytes(entry.size)) +
          "</span>" +
          '<span class="preview-date">' +
          esc(fmtPreviewDate(entry.modified)) +
          "</span>" +
          '<button type="button" class="preview-copy" data-preview-copy="' +
          esc(entry.path) +
          '" title="相対パスをコピー">⧉</button></div>'
      );
    }
    virtual.style.height = total * PREVIEW_ROW_HEIGHT + "px";
    virtual.innerHTML =
      '<div class="preview-rows" style="transform: translateY(' + start * PREVIEW_ROW_HEIGHT + "px)\">" + rows.join("") + "</div>";
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
        fmtDuration(progress.elapsedMs) +
        (progress.etaMs > 0 ? "（残り約 " + fmtDuration(progress.etaMs) + "）" : "");
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

  // cacheSummaryNote はキャッシュ利用状況を1行にまとめる。0 件なら何も出さない。
  function cacheSummaryNote(cache) {
    if (!cache) return "";
    const reused = Number(cache.reusedItems) || 0;
    const cachedHashes = Number(cache.cachedHashes) || 0;
    if (reused <= 0 && cachedHashes <= 0) return "";
    const parts = [];
    if (reused > 0) parts.push("前回結果を再利用 " + reused + " 件");
    if (cachedHashes > 0) parts.push("ZIPハッシュ再利用 " + cachedHashes + " 件");
    return " / " + parts.join(" / ");
  }

  async function startScan(full) {
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
    state.detailToken++;
    state.previewToken++;
    state.preview = null;
    state.previewSearch = "";
    state.previewExpanded = new Set();
    state.previewScrollTop = 0;
    state.previewVisible = [];
    state.previewVisibleDirty = true;
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
    setStatus(full ? "完全再スキャン中..." : "スキャン中...");
    updateBusyUI();
    updateSelectionUI();
    try {
      const info = await api(full ? "Rescan" : "Scan", state.roots);
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
        state.lastCache = info.cache || null;
        renderRoots();
        renderSummary();
        renderWarnings();
        const cacheNote = cacheSummaryNote(info.cache);
        setStatus(
          "スキャン完了: " +
            (info.summary.total || 0) +
            " 件 / 削除候補 " +
            (info.summary.candidateCount || 0) +
            " 件 / " +
            fmtBytes(info.summary.candidateBytes || 0) +
            cacheNote
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

  function moveKindLabel(kind) {
    return kind === "zip" ? "アーカイブ" : "フォルダ";
  }

  function methodLabel(method) {
    return MOVE_METHOD_LABEL[method] || method;
  }

  function targetsForOption(option, method) {
    const targets = option.targets || [];
    if (method === "folder") return targets.filter((target) => target.kind === "folder");
    if (method === "zip") return targets.filter((target) => target.kind === "zip");
    if (method === "both") return targets.filter((target) => target.kind === "folder" || target.kind === "zip");
    return [];
  }

  async function beginDelete() {
    if (state.busy || state.selected.size === 0) return;
    clearPendingDragSelection();
    const ids = Array.from(state.selected.keys());
    state.busy = true;
    updateBusyUI();
    setStatus("移動方法を確認しています...");
    try {
      const options = await api("GetMoveOptions", ids);
      if (!Array.isArray(options) || options.length === 0) {
        toast("移動できる項目がありません。再スキャンしてください。", true);
        return;
      }
      state.moveOptions = options;
      state.moveMethods = new Map();
      options.forEach((option) => {
        if (option.defaultMethod) state.moveMethods.set(option.id, option.defaultMethod);
      });
      state.movePreview = null;
      state.moveRequests = [];
      showMoveMethodDialog();
    } catch (error) {
      toast("移動方法の取得に失敗しました: " + error.message, true);
    } finally {
      state.busy = false;
      updateBusyUI();
      updateSelectionUI();
    }
  }

  function showMoveMethodDialog() {
    const options = state.moveOptions || [];
    const cards = options
      .map((option) => {
        const method = state.moveMethods.get(option.id) || "";
        const targets = option.targets || [];
        const targetRows = targets
          .map(
            (target) =>
              '<li><span class="move-kind">' +
              esc(moveKindLabel(target.kind)) +
              "</span> " +
              esc(target.path) +
              ' <span class="muted">' +
              esc(fmtBytes(target.size)) +
              "</span></li>"
          )
          .join("");
        const radio = (value, label, disabled, className) =>
          '<label class="move-choice ' +
          (className || "") +
          (disabled ? " disabled" : "") +
          '"><input type="radio" name="move-method-' +
          esc(option.id) +
          '" data-move-method="' +
          esc(option.id) +
          '" value="' +
          value +
          '"' +
          (method === value ? " checked" : "") +
          (disabled ? " disabled" : "") +
          ">" +
          esc(label) +
          "</label>";
        return (
          '<div class="move-card" data-move-card="' +
          esc(option.id) +
          '"><div class="move-card-head"><strong>' +
          esc(option.name) +
          "</strong>" +
          verdictBadge(option) +
          "</div>" +
          (targetRows
            ? '<ul class="move-targets">' + targetRows + "</ul>"
            : '<p class="result-fail">移動できる既存パスがありません。</p>') +
          '<div class="move-choices">' +
          radio("folder", "解凍済みフォルダのみ", !option.canFolder, "") +
          radio("zip", "アーカイブのみ", !option.canZip, "") +
          radio("both", "アーカイブとフォルダの両方（危険）", !option.canBoth, "danger") +
          "</div>" +
          (option.warning ? '<p class="move-warning">' + esc(option.warning) + "</p>" : "") +
          "</div>"
        );
      })
      .join("");
    const body =
      '<p class="muted">移動する対象を項目ごとに選択してください。ZIPとフォルダの両方を移動すると、最後のローカルコピーを失う可能性があります。</p>' +
      '<div class="move-list">' +
      cards +
      "</div>" +
      '<div id="moveBothWarning" class="modal-note hidden">ZIPとフォルダの両方を移動する項目があります。この操作は転送を行わず、最後のローカルコピーを失う可能性があります。次の最終確認でも明示的に承認してください。</div>' +
      '<div id="moveSummary" class="transfer-selection-summary"></div>';
    openModal({
      title: "ごみ箱へ移動する対象を選択",
      body,
      wide: true,
      actions: [
        { label: "キャンセル", className: "btn", onClick: closeModal },
        { label: "最終確認へ", id: "moveConfirmBtn", className: "btn primary", onClick: confirmMoveMethods },
      ],
    });
    updateMoveAggregate();
  }

  function buildMoveRequests() {
    const requests = [];
    let valid = true;
    for (const option of state.moveOptions || []) {
      const method = state.moveMethods.get(option.id) || "";
      if (!method) {
        valid = false;
        continue;
      }
      const targets = targetsForOption(option, method);
      if (targets.length === 0) {
        valid = false;
        continue;
      }
      requests.push({ id: option.id, method });
    }
    return { requests, valid: valid && requests.length > 0 };
  }

  function updateMoveAggregate() {
    const { requests, valid } = buildMoveRequests();
    let targetCount = 0;
    let bytes = 0;
    for (const request of requests) {
      const option = state.moveOptions.find((item) => item.id === request.id);
      if (!option) continue;
      const targets = targetsForOption(option, request.method);
      targetCount += targets.length;
      targets.forEach((target) => {
        bytes += Number(target.size) || 0;
      });
    }
    const summary = document.getElementById("moveSummary");
    if (summary) {
      summary.textContent =
        "移動対象: " + requests.length + " 項目 / " + targetCount + " 件 / " + fmtBytes(bytes);
    }
    const bothWarning = document.getElementById("moveBothWarning");
    const hasBoth = requests.some((request) => request.method === "both");
    if (bothWarning) bothWarning.classList.toggle("hidden", !hasBoth);
    const confirm = document.getElementById("moveConfirmBtn");
    if (confirm) confirm.disabled = !valid || targetCount === 0;
    state.moveRequests = requests;
  }

  async function confirmMoveMethods() {
    const { requests, valid } = buildMoveRequests();
    if (!valid || requests.length === 0) {
      toast("移動する対象と方法を選択してください", true);
      return;
    }
    const confirm = document.getElementById("moveConfirmBtn");
    if (confirm) confirm.disabled = true;
    try {
      const preview = await api("PreviewMove", requests);
      state.movePreview = preview;
      state.moveRequests = requests;
      showMoveFinalDialog(preview, requests);
    } catch (error) {
      toast("最終確認の取得に失敗しました: " + error.message, true);
      if (confirm) confirm.disabled = false;
    }
  }

  function showMoveFinalDialog(preview, requests) {
    const hasBoth = requests.some((request) => request.method === "both");
    const warnings = Array.isArray(preview.warnings) ? preview.warnings : [];
    const items = (preview.items || [])
      .map((item) => {
        const targets = (item.targets || [])
          .map(
            (target) =>
              '<li><span class="move-kind">' +
              esc(moveKindLabel(target.kind)) +
              "</span> " +
              esc(target.path) +
              ' <span class="muted">' +
              esc(fmtBytes(target.size)) +
              "</span></li>"
          )
          .join("");
        return (
          '<div class="move-final-item"><div class="move-final-head"><strong>' +
          esc(item.name) +
          "</strong><span>" +
          esc(methodLabel(item.method)) +
          "</span></div><ul class=\"move-targets\">" +
          targets +
          "</ul></div>"
        );
      })
      .join("");
    let body =
      '<div class="stat-row">' +
      '<div class="stat"><div class="stat-label">対象項目</div><div class="stat-value">' + (preview.itemCount || 0) + "</div></div>" +
      '<div class="stat"><div class="stat-label">移動対象</div><div class="stat-value">' + (preview.targetCount || 0) + " 件</div></div>" +
      '<div class="stat"><div class="stat-label">合計サイズ</div><div class="stat-value">' + esc(fmtBytes(preview.bytes || 0)) + "</div></div>" +
      "</div>" +
      '<p class="modal-note">Windowsのごみ箱へ移動します。復元できますが、完全削除へのフォールバックは行いません。移動直前に再検証し、変化した対象はスキップします。</p>' +
      (warnings.length
        ? '<ul class="modal-list move-warnings">' + warnings.map((warning) => "<li>" + esc(warning) + "</li>").join("") + "</ul>"
        : "") +
      '<div class="detail-title" style="margin-top:14px">移動対象のパス</div>' +
      '<div class="move-final-list">' + items + "</div>";
    if (hasBoth) {
      body +=
        '<label class="move-ack"><input type="checkbox" id="moveBothAck"> ZIPとフォルダの両方をごみ箱へ移動し、最後のローカルコピーを失う可能性があることを理解しました。</label>';
    }
    openModal({
      title: "ごみ箱への移動の最終確認",
      body,
      wide: true,
      actions: [
        { label: "戻る", className: "btn", onClick: showMoveMethodDialog },
        { label: "ごみ箱に移動", id: "moveExecuteBtn", className: "btn danger", onClick: () => executeMove(requests) },
      ],
    });
    updateMoveFinalButton();
  }

  function updateMoveFinalButton() {
    const ack = document.getElementById("moveBothAck");
    const button = document.getElementById("moveExecuteBtn");
    if (button) button.disabled = !!(ack && !ack.checked);
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

  async function executeMove(requests) {
    if (!requests || requests.length === 0) return;
    state.busy = true;
    state.deleting = true;
    state.deleteTotal = requests.length;
    updateBusyUI();
    setStatus("ごみ箱に移動しています...");
    openModal({
      title: "ごみ箱に移動しています",
      body: deleteProgressBody({ index: 0, total: requests.length, stage: "revalidating", name: "" }),
      actions: [],
      closable: false,
    });
    try {
      const response = await api("DeleteSelected", requests, true);
      state.deleting = false;
      if (response && response.summary) state.summary = response.summary;
      renderSummary();
      state.selected.clear();
      clearPendingDragSelection();
      state.activeId = null;
      state.detailToken++;
      state.previewToken++;
      state.preview = null;
      state.previewSearch = "";
      state.previewExpanded = new Set();
      state.previewScrollTop = 0;
      state.previewVisible = [];
      state.previewVisibleDirty = true;
      state.detailItem = null;
      detailPane.innerHTML =
        '<div class="detail-empty"><div class="detail-title">詳細</div><p>行を選択すると、ZIPとフォルダの対応・一致根拠・差異を表示します。</p></div>';
      updateSelectionUI();
      await reloadRows(false);
      const succeeded = response ? response.succeeded || 0 : 0;
      const partialCount = response ? response.partial || 0 : 0;
      const failedCount = response ? response.failed || 0 : 0;
      const skippedCount = response ? response.skipped || 0 : 0;
      if (succeeded > 0 || partialCount > 0) {
        toast(
          "ごみ箱移動: 成功 " +
            succeeded +
            " / 一部成功 " +
            partialCount +
            " / 失敗 " +
            failedCount +
            " / スキップ " +
            skippedCount
        );
      } else {
        toast("ごみ箱に移動された項目はありません", true);
      }
      setStatus(
        "移動完了: 成功 " +
          succeeded +
          " / 一部成功 " +
          partialCount +
          " / 失敗 " +
          failedCount +
          " / スキップ " +
          skippedCount
      );
      showMoveResult(response);
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

  function showMoveResult(response) {
    if (!response) return;
    const results = response.results || [];
    const problems = results.filter((item) => !item.success && !item.skipped);
    const skipped = results.filter((item) => item.skipped);
    let body =
      '<div class="stat-row">' +
      '<div class="stat"><div class="stat-label">成功</div><div class="stat-value result-ok">' + (response.succeeded || 0) + "</div></div>" +
      '<div class="stat"><div class="stat-label">一部成功</div><div class="stat-value" style="color:var(--orange)">' + (response.partial || 0) + "</div></div>" +
      '<div class="stat"><div class="stat-label">失敗</div><div class="stat-value result-fail">' + (response.failed || 0) + "</div></div>" +
      '<div class="stat"><div class="stat-label">スキップ</div><div class="stat-value">' + (response.skipped || 0) + "</div></div>" +
      "</div>";
    if (problems.length) {
      body +=
        '<div class="detail-title" style="margin-top:14px">失敗・一部成功の項目</div><ul class="modal-list">' +
        problems
          .slice(0, 100)
          .map((item) => {
            const targets = (item.targets || [])
              .map((target) => {
                const mark = target.success ? "移動済み" : "失敗: " + target.reason;
                return esc(moveKindLabel(target.kind)) + " " + esc(target.path) + " — " + esc(mark);
              })
              .join("<br>");
            return (
              "<li><strong>" +
              esc(item.name) +
              "</strong>（" +
              esc(methodLabel(item.method)) +
              "）<br>" +
              esc(item.reason) +
              (targets ? "<br>" + targets : "") +
              "</li>"
            );
          })
          .join("") +
        "</ul>";
    }
    if (skipped.length) {
      body +=
        '<div class="detail-title" style="margin-top:14px">スキップした項目</div><ul class="modal-list">' +
        skipped
          .slice(0, 50)
          .map((item) => "<li>" + esc(item.name || item.id) + " — " + esc(item.reason) + "</li>")
          .join("") +
        "</ul>";
    }
    body += '<p class="muted" style="margin-top:10px">操作履歴: ' + esc(state.logPath || "未設定") + "</p>";
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
    const rescanBtn = el("rescanBtn");
    if (rescanBtn) rescanBtn.disabled = state.busy || state.roots.length === 0;
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
    scanBtn.addEventListener("click", () => startScan(false));
    const rescanBtn = el("rescanBtn");
    if (rescanBtn) rescanBtn.addEventListener("click", () => startScan(true));
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
      if (state.dragSelect) {
        const drag = state.dragSelect;
        const index = dragIndexAtPoint(drag.clientX, drag.clientY);
        if (index != null) applyDragIndex(index);
      }
      scheduleRender();
    });
    window.addEventListener("resize", scheduleRender);
    window.addEventListener("blur", cancelDragSelection);
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) cancelDragSelection();
    });

    rowsEl.addEventListener("pointerdown", startDragSelection);

    rowsEl.addEventListener("click", (event) => {
      if (state.dragSelect || performance.now() < state.suppressRowClickUntil) {
        state.suppressRowClickUntil = 0;
        if (event.target.matches && event.target.matches('input[type="checkbox"]')) event.preventDefault();
        return;
      }
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
      if (!id) return;
      if (state.dragSelect || performance.now() < state.suppressCheckboxChangeUntil) {
        state.suppressCheckboxChangeUntil = 0;
        event.target.checked = state.selected.has(id);
        return;
      }
      const rowElement = event.target.closest(".row");
      const index = rowElement ? Number(rowElement.dataset.index) : null;
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
      if (state.dragSelect || performance.now() < state.suppressRowClickUntil) return;
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
      const previewCopy = event.target.closest("[data-preview-copy]");
      if (previewCopy) {
        copyText(previewCopy.dataset.previewCopy);
        return;
      }
      const previewToggle = event.target.closest("[data-preview-toggle]");
      if (previewToggle) {
        const path = previewToggle.dataset.previewToggle;
        if (state.previewExpanded.has(path)) state.previewExpanded.delete(path);
        else state.previewExpanded.add(path);
        state.previewVisibleDirty = true;
        renderPreviewList();
        return;
      }
      const previewTab = event.target.closest("[data-preview-source]");
      if (previewTab && state.activeId && state.preview) {
        const source = previewTab.dataset.previewSource;
        if (source && source !== state.preview.source) {
          const token = ++state.previewToken;
          state.previewScrollTop = 0;
          loadPreview(state.activeId, source, token);
        }
        return;
      }
      const copyButton = event.target.closest('[data-action="copy"]');
      if (copyButton) {
        const item = state.detailItem;
        if (item) copyText(item[copyButton.dataset.field]);
        return;
      }
      const folderButton = event.target.closest('[data-action="reveal-folder"]');
      if (folderButton) {
        api("RevealFolder", folderButton.dataset.id).catch((error) => toast(error.message, true));
        return;
      }
      const aiButton = event.target.closest('[data-action="ai-suggest"]');
      if (aiButton) {
        requestSuggestion(aiButton.dataset.id, aiButton);
        return;
      }
      const zipButton = event.target.closest('[data-action="reveal-zip"]');
      if (zipButton) {
        api("RevealZip", zipButton.dataset.id).catch((error) => toast(error.message, true));
        return;
      }
      const button = event.target.closest('[data-action="reveal"]');
      if (!button) return;
      api("RevealItem", button.dataset.id).catch((error) => toast(error.message, true));
    });

    detailPane.addEventListener("input", (event) => {
      if (event.target.id !== "previewSearch") return;
      state.previewSearch = event.target.value;
      state.previewScrollTop = 0;
      state.previewVisibleDirty = true;
      const list = document.getElementById("previewList");
      if (list) list.scrollTop = 0;
      renderPreviewList();
    });

    detailPane.addEventListener(
      "scroll",
      (event) => {
        if (!event.target || event.target.id !== "previewList") return;
        state.previewScrollTop = event.target.scrollTop;
        renderPreviewList();
      },
      true
    );

    selectAllBtn.addEventListener("click", selectAllCandidates);
    clearSelectionBtn.addEventListener("click", () => {
      state.selected.clear();
      clearPendingDragSelection();
      state.lastRowIndex = null;
      updateSelectionUI();
      renderRows();
    });
    resetColumnsBtn.addEventListener("click", resetColumnWidths);
    const exportCsvBtn = el("exportCsvBtn");
    const exportJsonBtn = el("exportJsonBtn");
    if (exportCsvBtn) exportCsvBtn.addEventListener("click", () => exportReport("csv", exportCsvBtn));
    if (exportJsonBtn) exportJsonBtn.addEventListener("click", () => exportReport("json", exportJsonBtn));
    transferBtn.addEventListener("click", openTransferReview);
    deleteBtn.addEventListener("click", beginDelete);
    warningBadge.addEventListener("click", showWarnings);
    modalClose.addEventListener("click", closeModal);
    modalBody.addEventListener("change", (event) => {
      if (event.target.matches("[data-move-method]")) {
        const id = event.target.dataset.moveMethod;
        if (!id) return;
        state.moveMethods.set(id, event.target.value);
        updateMoveAggregate();
        return;
      }
      if (event.target.id === "moveBothAck") {
        updateMoveFinalButton();
        return;
      }
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
    const dashboardToggle = el("dashboardToggle");
    const dashboardPanel = el("dashboardPanel");
    if (dashboardToggle && dashboardPanel) {
      dashboardToggle.addEventListener("click", () => {
        state.dashboardCollapsed = !state.dashboardCollapsed;
        dashboardPanel.classList.toggle("collapsed", state.dashboardCollapsed);
        dashboardToggle.textContent = state.dashboardCollapsed ? "+" : "−";
        dashboardToggle.setAttribute("aria-label", state.dashboardCollapsed ? "展開する" : "折りたたむ");
      });
    }

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
    setupLayoutSizing();
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
        state.workers = Number(info.workers) || 0;
        state.ai = info.ai || null;
        state.cacheInfo = info.cache || null;
        state.archiveInfo = info.archive || null;
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
