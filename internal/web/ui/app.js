"use strict";

const api = {
  // Resolve API paths RELATIVE to the page so the UI works behind HA Ingress (served at
  // /api/hassio_ingress/<token>/) as well as on the raw port. Strips the leading slash.
  rel(u) { return u.replace(/^\//, ""); },
  async get(u) { return handle(await fetch(this.rel(u))); },
  async send(method, u, body) {
    return handle(await fetch(this.rel(u), {
      method,
      headers: { "Content-Type": "application/json" },
      body: body == null ? undefined : JSON.stringify(body),
    }));
  },
  post(u, b) { return this.send("POST", u, b); },
  put(u, b) { return this.send("PUT", u, b); },
  del(u) { return this.send("DELETE", u); },
};

async function handle(resp) {
  const text = await resp.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!resp.ok) {
    const msg = (data && data.error) ? data.error : ("HTTP " + resp.status);
    throw new Error(msg);
  }
  return data;
}

function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, c =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
function fmtTime(s) {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d)) return "—";
  return d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
function $(id) { return document.getElementById(id); }

let toastTimer;
function toast(msg, isErr) {
  const t = $("toast");
  t.textContent = msg;
  t.className = "toast show" + (isErr ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = "toast"; }, 3500);
}

// ---- tab routing ----
document.querySelectorAll("nav button").forEach(b => {
  b.addEventListener("click", () => showTab(b.dataset.tab));
});
function showTab(name) {
  document.querySelectorAll("nav button").forEach(b => b.classList.toggle("active", b.dataset.tab === name));
  document.querySelectorAll("main section").forEach(s => s.classList.toggle("active", s.id === name));
  const fn = { dashboard: App.refreshDashboard, sources: App.refreshSources, filters: App.refreshFilters, activity: App.refreshActivity, settings: App.loadSettings }[name];
  if (fn) fn.call(App); // bind `this` to App — refreshSources() calls this.renderSources()
}

const App = {
  // ---- dashboard ----
  async refreshDashboard() {
    try {
      const r = await api.post("/api/settings/test", {});
      $("conn-status").innerHTML = `<span class="statusdot ok"></span> Connected to Tandoor — ${r.recipe_count} recipes`;
    } catch (e) {
      $("conn-status").innerHTML = `<span class="statusdot error"></span> Tandoor: ${esc(e.message)} <span class="muted">(set it in Settings)</span>`;
    }
    try {
      const data = await api.get("/api/items?limit=10");
      const c = data.counts || {};
      const order = ["imported", "filtered_out", "duplicate", "failed", "skipped"];
      $("stats").innerHTML = order.map(k =>
        `<div class="stat"><span class="n">${c[k] || 0}</span><span class="l">${k.replace("_", " ")}</span></div>`).join("");
      $("dash-activity").innerHTML = itemsTable(data.items);
    } catch (e) { toast(e.message, true); }
  },

  // ---- sources ----
  _sources: [],
  _editingId: null,
  async refreshSources() {
    try {
      App._sources = await api.get("/api/sources");
      this.renderSources();
    } catch (e) { toast(e.message, true); }
  },
  renderSources() {
    const srcs = App._sources || [];
    if (!srcs.length) { $("sources-list").innerHTML = `<div class="panel muted">No sources yet. Add one below.</div>`; return; }
    $("sources-list").innerHTML = srcs.map(srcRow).join("");
  },
  editSource(id) { App._editingId = id; this.renderSources(); },
  cancelEdit() { App._editingId = null; this.renderSources(); },
  async saveSourceEdit(id) {
    const g = sfx => $(`edit-${sfx}-${id}`);
    const body = {
      name: g("name").value.trim(),
      feed_url: g("feed").value.trim(),
      site_url: g("site").value.trim(),
      poll_interval_minutes: parseInt(g("interval").value) || 60,
      default_keywords: g("keywords").value.trim(),
      backlog_limit: parseInt(g("backlog").value) || 25,
      backlog_on_add: g("backlogon").checked,
      enabled: g("enabled").checked,
    };
    if (!body.feed_url) { toast("Feed URL is required", true); return; }
    try { await api.put(`/api/sources/${id}`, body); App._editingId = null; this.refreshSources(); toast("Source updated"); }
    catch (e) { toast(e.message, true); }
  },
  async addSource() {
    const body = {
      name: $("src-name").value.trim(),
      feed_url: $("src-feed").value.trim(),
      site_url: $("src-site").value.trim(),
      poll_interval_minutes: parseInt($("src-interval").value) || 60,
      default_keywords: $("src-keywords").value.trim(),
      backlog_on_add: $("src-backlog").checked,
      backlog_limit: parseInt($("src-backlog-limit").value) || 25,
      enabled: true,
    };
    if (!body.feed_url) { toast("Feed URL is required", true); return; }
    try {
      await api.post("/api/sources", body);
      toast("Source added" + (body.backlog_on_add ? " — importing backlog in background" : " — watching for new recipes"));
      ["src-name", "src-feed", "src-site", "src-keywords"].forEach(id => $(id).value = "");
      this.refreshSources();
    } catch (e) { toast(e.message, true); }
  },
  async runSource(id, backlog) {
    try { await api.post(`/api/sources/${id}/run${backlog ? "?backlog=1" : ""}`); toast("Run started — check Activity"); }
    catch (e) { toast(e.message, true); }
  },
  async toggleSource(id, enabled) {
    try {
      const s = await api.get("/api/sources").then(l => l.find(x => x.id === id));
      s.enabled = enabled;
      await api.put(`/api/sources/${id}`, s);
      this.refreshSources();
    } catch (e) { toast(e.message, true); }
  },
  async deleteSource(id) {
    if (!confirm("Delete this source? (Imported recipes stay in Tandoor.)")) return;
    try { await api.del(`/api/sources/${id}`); this.refreshSources(); } catch (e) { toast(e.message, true); }
  },

  // ---- filters ----
  async refreshFilters() {
    try {
      const [rules, srcs] = await Promise.all([api.get("/api/rules"), api.get("/api/sources")]);
      const sel = $("rule-source");
      sel.innerHTML = `<option value="">Global (all sources)</option>` +
        srcs.map(s => `<option value="${s.id}">${esc(s.name)}</option>`).join("");
      const nameById = Object.fromEntries(srcs.map(s => [s.id, s.name]));
      if (!rules.length) { $("rules-list").innerHTML = `<div class="panel muted">No rules — everything passes.</div>`; return; }
      $("rules-list").innerHTML = `<div class="panel"><table><thead><tr><th>Mode</th><th>Field</th><th>Keyword</th><th>Match</th><th>Scope</th><th></th></tr></thead><tbody>` +
        rules.map(r => `<tr>
          <td><span class="pill ${r.mode === "blacklist" ? "failed" : "imported"}">${r.mode}</span></td>
          <td>${esc(r.field)}</td><td><strong>${esc(r.keyword)}</strong></td><td>${esc(r.match_type)}</td>
          <td>${r.source_id ? esc(nameById[r.source_id] || ("#" + r.source_id)) : "Global"}</td>
          <td style="text-align:right"><button class="btn danger" onclick="App.deleteRule(${r.id})">Delete</button></td>
        </tr>`).join("") + `</tbody></table></div>`;
    } catch (e) { toast(e.message, true); }
  },
  async addRule() {
    const sourceVal = $("rule-source").value;
    const body = {
      mode: $("rule-mode").value,
      field: $("rule-field").value,
      keyword: $("rule-keyword").value.trim(),
      match_type: $("rule-match").value,
      source_id: sourceVal ? parseInt(sourceVal) : null,
      enabled: true,
    };
    if (!body.keyword) { toast("Keyword is required", true); return; }
    try { await api.post("/api/rules", body); $("rule-keyword").value = ""; this.refreshFilters(); toast("Rule added"); }
    catch (e) { toast(e.message, true); }
  },
  async deleteRule(id) {
    try { await api.del(`/api/rules/${id}`); this.refreshFilters(); } catch (e) { toast(e.message, true); }
  },

  // ---- activity ----
  async refreshActivity() {
    try {
      const status = $("activity-filter").value;
      const data = await api.get(`/api/items?limit=100${status ? "&status=" + status : ""}`);
      $("activity-list").innerHTML = itemsTable(data.items);
    } catch (e) { toast(e.message, true); }
  },
  async importOne() {
    const url = $("import-url").value.trim();
    if (!url) { toast("Enter a URL", true); return; }
    toast("Importing…");
    try {
      const it = await api.post("/api/import", { url });
      toast(`${it.status}: ${it.title || url}${it.filter_reason ? " — " + it.filter_reason : ""}${it.error ? " — " + it.error : ""}`, it.status !== "imported");
      $("import-url").value = "";
      this.refreshActivity();
    } catch (e) { toast(e.message, true); }
  },
  async importBulk() {
    const urls = $("import-bulk").value.split("\n").map(s => s.trim()).filter(Boolean);
    if (!urls.length) { toast("Paste some URLs", true); return; }
    try {
      const r = await api.post("/api/import", { urls });
      toast(`Importing ${r.count} URLs in the background — check Activity`);
      $("import-bulk").value = "";
    } catch (e) { toast(e.message, true); }
  },

  // ---- settings ----
  async loadSettings() {
    try {
      const s = await api.get("/api/settings");
      $("set-url").value = s.tandoor_url || "";
      $("set-token").placeholder = s.token_set ? "•••••• (set — blank to keep)" : "paste API token";
      $("en-tags").checked = s.enrich_curate_tags;
      $("en-desc").checked = s.enrich_clean_description;
      $("en-food").checked = s.enrich_communize;
      $("en-steps").checked = s.enrich_allocate_steps;
      $("en-descmode").value = s.enrich_description_mode || "clean";
      $("en-denylist").value = (s.enrich_tag_denylist || []).join(", ");
      $("opt-books").checked = s.organize_into_books;
    } catch (e) { toast(e.message, true); }
  },
  async saveSettings() {
    const body = {
      tandoor_url: $("set-url").value.trim(),
      tandoor_token: $("set-token").value.trim(),
      enrich_curate_tags: $("en-tags").checked,
      enrich_clean_description: $("en-desc").checked,
      enrich_communize: $("en-food").checked,
      enrich_allocate_steps: $("en-steps").checked,
      enrich_description_mode: $("en-descmode").value,
      enrich_tag_denylist: $("en-denylist").value.split(",").map(s => s.trim()).filter(Boolean),
      organize_into_books: $("opt-books").checked,
    };
    try { await api.put("/api/settings", body); $("set-token").value = ""; this.loadSettings(); toast("Settings saved"); }
    catch (e) { toast(e.message, true); }
  },
  async testConnection() {
    const el = $("set-test-result");
    el.textContent = "testing…";
    try {
      const r = await api.post("/api/settings/test", { tandoor_url: $("set-url").value.trim(), tandoor_token: $("set-token").value.trim() });
      el.innerHTML = `<span class="statusdot ok"></span> OK — ${r.recipe_count} recipes`;
    } catch (e) { el.innerHTML = `<span class="statusdot error"></span> ${esc(e.message)}`; }
  },

  // ---- reprocess existing recipes ----
  _repro: [],
  async reprocessScan() {
    $("repro-status").textContent = "Scanning your library… (this can take a minute)";
    $("repro-apply").disabled = true;
    try {
      const r = await api.post("/api/reprocess", { mode: "preview" });
      App._repro = r.items || [];
      const s = r.summary || {};
      const fixable = App._repro.filter(i => !i.skip);
      $("repro-status").textContent =
        `Scanned ${s.scanned}: ${s.fixable} fixable, ${s.componentized} componentized (skipped).`;
      $("repro-apply").disabled = fixable.length === 0;
      this.renderRepro();
    } catch (e) { $("repro-status").textContent = ""; toast(e.message, true); }
  },
  renderRepro() {
    const fixable = App._repro.filter(i => !i.skip);
    const comp = App._repro.filter(i => i.skip === "componentized");
    let html = "";
    if (fixable.length) {
      html += `<div class="panel"><strong>${fixable.length} fixable</strong>
        <table><thead><tr><th>Recipe</th><th>Moved</th><th>Cleaned</th><th>Headers</th><th></th></tr></thead><tbody>` +
        fixable.map(i => `<tr><td>${esc(i.name)}</td><td>${i.moved}</td><td>${i.communized}</td><td>${i.headers}</td>
          <td>${i.applied ? '<span class="pill imported">done</span>' : (i.error ? '<span class="pill failed">' + esc(i.error) + '</span>' : '')}</td></tr>`).join("") +
        `</tbody></table></div>`;
    }
    if (comp.length) {
      html += `<div class="panel muted"><strong>${comp.length} componentized — skipped</strong>
        (these have ingredient sections; re-import to fix them): ` + comp.map(i => esc(i.name)).join(", ") + `</div>`;
    }
    $("repro-results").innerHTML = html || `<div class="panel muted">Nothing to change — your library is already up to date.</div>`;
  },
  async reprocessApply() {
    const fixable = App._repro.filter(i => !i.skip && !i.applied && !i.error);
    if (!fixable.length) { toast("Nothing to apply — scan first", true); return; }
    if (!confirm(`Reprocess ${fixable.length} recipe(s)? Each is backed up to the add-on's data folder first.`)) return;
    $("repro-apply").disabled = true;
    let done = 0, failed = 0;
    for (let i = 0; i < fixable.length; i += 25) {
      const ids = fixable.slice(i, i + 25).map(x => x.id);
      $("repro-status").textContent = `Applying ${done}/${fixable.length}…`;
      try {
        const r = await api.post("/api/reprocess", { mode: "apply", ids });
        done += (r.summary && r.summary.applied) || 0;
        failed += (r.summary && r.summary.failed) || 0;
      } catch (e) { toast(e.message, true); break; }
    }
    $("repro-status").textContent = `Done — reprocessed ${done} recipe(s)${failed ? ", " + failed + " failed" : ""}. Re-scanning…`;
    toast(`Reprocessed ${done} recipe(s)`);
    this.reprocessScan();
  },
};

function srcRow(s) {
  if (App._editingId === s.id) {
    return `<div class="panel">
      <div class="grid cols-2">
        <div><label>Name</label><input id="edit-name-${s.id}" value="${esc(s.name)}"></div>
        <div><label>Feed URL</label><input id="edit-feed-${s.id}" value="${esc(s.feed_url)}"></div>
        <div><label>Site URL</label><input id="edit-site-${s.id}" value="${esc(s.site_url)}"></div>
        <div><label>Poll interval (minutes)</label><input id="edit-interval-${s.id}" type="number" value="${s.poll_interval_minutes}"></div>
        <div><label>Default keywords</label><input id="edit-keywords-${s.id}" value="${esc(s.default_keywords)}"></div>
        <div><label>Backlog limit</label><input id="edit-backlog-${s.id}" type="number" value="${s.backlog_limit}"></div>
      </div>
      <div class="row" style="margin-top:12px">
        <label class="muted" style="margin:0"><input type="checkbox" id="edit-enabled-${s.id}" ${s.enabled ? "checked" : ""}> enabled</label>
        <label class="muted" style="margin:0"><input type="checkbox" id="edit-backlogon-${s.id}" ${s.backlog_on_add ? "checked" : ""}> import recent on add</label>
        <div class="spacer"></div>
        <button class="btn" onclick="App.saveSourceEdit(${s.id})">Save</button>
        <button class="btn secondary" onclick="App.cancelEdit()">Cancel</button>
      </div>
    </div>`;
  }
  const dot = s.last_status === "ok" ? "ok" : (s.last_status === "error" ? "error" : "unknown");
  return `<div class="panel">
    <div class="row">
      <div style="flex:1">
        <div><strong>${esc(s.name)}</strong> ${s.enabled ? "" : '<span class="pill skipped">disabled</span>'}</div>
        <div class="itemurl">${esc(s.feed_url)}</div>
        <div class="muted" style="font-size:12px;margin-top:4px">
          <span class="statusdot ${dot}"></span>${esc(s.last_status || "never run")}${s.last_error ? " — " + esc(s.last_error) : ""}
          · every ${s.poll_interval_minutes}m · last: ${fmtTime(s.last_checked_at)}
        </div>
      </div>
      <button class="btn secondary" onclick="App.editSource(${s.id})">Edit</button>
      <button class="btn secondary" onclick="App.runSource(${s.id}, false)">Run now</button>
      <button class="btn secondary" onclick="App.runSource(${s.id}, true)">Import backlog</button>
      <label class="muted" style="margin:0"><input type="checkbox" ${s.enabled ? "checked" : ""} onchange="App.toggleSource(${s.id}, this.checked)"> on</label>
      <button class="btn danger" onclick="App.deleteSource(${s.id})">Delete</button>
    </div>
  </div>`;
}

function itemsTable(items) {
  if (!items || !items.length) return `<div class="muted">Nothing yet.</div>`;
  return `<table><thead><tr><th>Status</th><th>Recipe</th><th>Detail</th><th>When</th></tr></thead><tbody>` +
    items.map(it => `<tr>
      <td><span class="pill ${esc(it.status)}">${esc(it.status)}</span></td>
      <td><div>${esc(it.title || "(untitled)")}</div><div class="itemurl"><a href="${esc(it.url)}" target="_blank" rel="noopener">${esc(it.url)}</a></div></td>
      <td class="muted">${esc(it.filter_reason || it.error || (it.tandoor_recipe_id ? "recipe #" + it.tandoor_recipe_id : ""))}</td>
      <td class="muted" style="white-space:nowrap">${fmtTime(it.processed_at || it.discovered_at)}</td>
    </tr>`).join("") + `</tbody></table>`;
}

App.refreshDashboard();
