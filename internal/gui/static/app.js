function app() {
  return {
    // ── State ──────────────────────────────────────────────────────────────
    contexts: [],
    files: [],
    selected: null,
    search: '',
    filterGroup: '',
    filterFile: '',
    pinnedOnly: false,
    sortField: 'name',
    sortDir: 'asc',
    currentCtx: '',

    // quick switcher
    showSearch: false,
    quickSearch: '',
    quickResults: [],
    quickIdx: 0,

    // drawer
    groupInput: '',
    healthResult: null,

    // toasts
    toasts: [],
    _toastId: 0,

    // theme
    theme: localStorage.getItem('kcm-theme') || 'dark',

    // ── Init ───────────────────────────────────────────────────────────────
    async init() {
      document.documentElement.setAttribute('data-theme', this.theme);
      await Promise.all([this.loadContexts(), this.loadFiles()]);
      this.startSSE();
    },

    // ── Data loading ────────────────────────────────────────────────────────
    async loadContexts() {
      const params = new URLSearchParams();
      if (this.search)      params.set('q', this.search);
      if (this.filterGroup) params.set('group', this.filterGroup);
      if (this.filterFile)  params.set('file', this.filterFile);
      if (this.pinnedOnly)  params.set('pinned', '1');

      const res = await fetch('/api/contexts?' + params);
      if (!res.ok) return;
      const data = await res.json();
      this.contexts = data.contexts || [];
      this.currentCtx = this.contexts.find(c => c.current)?.name || '';
    },

    async loadFiles() {
      const res = await fetch('/api/files');
      if (!res.ok) return;
      const data = await res.json();
      this.files = data.files || [];
    },

    // ── Computed ────────────────────────────────────────────────────────────
    get sortedContexts() {
      return [...this.contexts].sort((a, b) => {
        let va = a[this.sortField] ?? '';
        let vb = b[this.sortField] ?? '';
        // current always first
        if (a.current) return -1;
        if (b.current) return  1;
        if (va < vb) return this.sortDir === 'asc' ? -1 :  1;
        if (va > vb) return this.sortDir === 'asc' ?  1 : -1;
        return 0;
      });
    },

    get pinned() {
      return this.contexts.filter(c => c.pinned);
    },

    get groupList() {
      const gs = new Set(this.contexts.map(c => c.group).filter(Boolean));
      return [...gs].sort();
    },

    groupCount(g) {
      return this.contexts.filter(c => c.group === g).length;
    },

    // ── Sorting ─────────────────────────────────────────────────────────────
    sortBy(field) {
      if (this.sortField === field) {
        this.sortDir = this.sortDir === 'asc' ? 'desc' : 'asc';
      } else {
        this.sortField = field;
        this.sortDir = 'asc';
      }
    },

    // ── Actions ─────────────────────────────────────────────────────────────
    async switchCtx(name) {
      const fd = new FormData();
      fd.append('name', name);
      const res = await fetch('/api/contexts/use', { method: 'POST', body: fd });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        this.toast('error', 'Failed: ' + (e.error || res.statusText));
        return;
      }
      this.toast('success', `Switched to "${name}"`);
      this.showSearch = false;
      await this.loadContexts();
      if (this.selected?.name === name) {
        this.selected = this.contexts.find(c => c.name === name) || null;
      }
    },

    async togglePin() {
      if (!this.selected) return;
      const fd = new FormData();
      fd.append('name', this.selected.name);
      fd.append('pinned', String(!this.selected.pinned));
      await fetch('/api/contexts/pin', { method: 'POST', body: fd });
      await this.loadContexts();
      this.selected = this.contexts.find(c => c.name === this.selected.name) || null;
    },

    async setGroup() {
      if (!this.selected) return;
      const fd = new FormData();
      fd.append('name', this.selected.name);
      fd.append('group', this.groupInput);
      await fetch('/api/contexts/group', { method: 'POST', body: fd });
      this.toast('success', 'Group updated');
      await this.loadContexts();
      this.selected = this.contexts.find(c => c.name === this.selected.name) || null;
    },

    async deleteCtx(name) {
      if (!confirm(`Delete context "${name}"?\n\nThis cannot be undone (but a backup will be taken).`)) return;
      const fd = new FormData();
      fd.append('name', name);
      const res = await fetch('/api/contexts/delete', { method: 'POST', body: fd });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        this.toast('error', 'Delete failed: ' + (e.error || res.statusText));
        return;
      }
      this.toast('success', `Deleted "${name}"`);
      this.selected = null;
      await this.loadContexts();
    },

    async promptRename() {
      if (!this.selected) return;
      const newName = prompt(`Rename "${this.selected.name}" to:`, this.selected.name);
      if (!newName || newName === this.selected.name) return;
      const fd = new FormData();
      fd.append('old', this.selected.name);
      fd.append('new', newName);
      const res = await fetch('/api/contexts/rename', { method: 'POST', body: fd });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        this.toast('error', 'Rename failed: ' + (e.error || res.statusText));
        return;
      }
      this.toast('success', `Renamed to "${newName}"`);
      await this.loadContexts();
      this.selected = this.contexts.find(c => c.name === newName) || null;
    },

    exportCtx(name) {
      window.open(`/api/contexts/export?name=${encodeURIComponent(name)}`, '_blank');
    },

    async checkHealth() {
      this.toast('success', 'Checking all clusters…');
      const res = await fetch('/api/health');
      if (!res.ok) return;
      const data = await res.json();
      const results = data.results || [];
      const ok = results.filter(r => r.OK).length;
      this.toast('success', `Health check complete: ${ok}/${results.length} reachable`);
    },

    async checkDetailHealth() {
      if (!this.selected) return;
      this.healthResult = null;
      const res = await fetch(`/api/health?context=${encodeURIComponent(this.selected.name)}`);
      if (!res.ok) return;
      this.healthResult = await res.json();
    },

    // ── Detail drawer ────────────────────────────────────────────────────────
    openDetail(c) {
      this.selected = c;
      this.groupInput = c.group || '';
      this.healthResult = null;
      this.checkDetailHealth();
    },

    // ── Quick switcher ────────────────────────────────────────────────────────
    openSearch() {
      this.showSearch = true;
      this.quickSearch = '';
      this.quickResults = this.contexts.slice(0, 12);
      this.quickIdx = 0;
      this.$nextTick(() => this.$refs.searchInput?.focus());
    },

    filterQuick() {
      const q = this.quickSearch.toLowerCase();
      if (!q) {
        this.quickResults = this.contexts.slice(0, 12);
        this.quickIdx = 0;
        return;
      }
      this.quickResults = this.contexts.filter(c =>
        c.name.toLowerCase().includes(q) ||
        (c.cluster || '').toLowerCase().includes(q) ||
        (c.group  || '').toLowerCase().includes(q)
      ).slice(0, 12);
      this.quickIdx = 0;
    },

    quickSelect() {
      const c = this.quickResults[this.quickIdx];
      if (c) { this.switchCtx(c.name); this.showSearch = false; }
    },

    closeAll() {
      if (this.showSearch) { this.showSearch = false; return; }
      if (this.selected)   { this.selected = null; }
    },

    // ── Filters ────────────────────────────────────────────────────────────
    clearFilters() {
      this.filterGroup = '';
      this.filterFile  = '';
      this.pinnedOnly  = false;
      this.loadContexts();
    },

    // ── Theme ─────────────────────────────────────────────────────────────
    toggleTheme() {
      this.theme = this.theme === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', this.theme);
      localStorage.setItem('kcm-theme', this.theme);
    },

    // ── SSE ───────────────────────────────────────────────────────────────
    startSSE() {
      const connect = () => {
        const es = new EventSource('/events');
        es.onmessage = async (e) => {
          if (e.data && !e.data.startsWith(':')) {
            await this.loadContexts();
            await this.loadFiles();
          }
        };
        es.onerror = () => { es.close(); setTimeout(connect, 5000); };
      };
      connect();
    },

    // ── Helpers ────────────────────────────────────────────────────────────
    fmtDate(iso) {
      try {
        const d = new Date(iso);
        const diff = Date.now() - d.getTime();
        if (diff < 60_000)     return 'just now';
        if (diff < 3_600_000)  return Math.floor(diff/60_000) + 'm ago';
        if (diff < 86_400_000) return Math.floor(diff/3_600_000) + 'h ago';
        return d.toLocaleDateString();
      } catch { return iso; }
    },

    fmtDateFull(iso) {
      try { return new Date(iso).toLocaleString(); } catch { return iso; }
    },

    shortPath(p) {
      if (!p) return '';
      const home = p.replace(/^\/Users\/[^/]+/, '~').replace(/^\/home\/[^/]+/, '~');
      return home.length < 30 ? home : '…/' + p.split('/').slice(-2).join('/');
    },

    cloudIcon(provider) {
      const icons = { aws: '☁', gcp: '🔵', azure: '🟦', digitalocean: '🌊' };
      return icons[provider?.toLowerCase()] || '☁';
    },

    // ── Toast ─────────────────────────────────────────────────────────────
    toast(type, msg) {
      const id = ++this._toastId;
      this.toasts.push({ id, type, msg });
      setTimeout(() => { this.toasts = this.toasts.filter(t => t.id !== id); }, 3500);
    },
  };
}
