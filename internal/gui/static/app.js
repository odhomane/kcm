function app() {
  return {
    contexts: [],
    files: [],
    groups: {},
    selected: null,
    search: '',
    filterGroup: '',
    filterFile: '',
    pinnedOnly: false,
    showSearch: false,
    quickSearch: '',
    quickResults: [],
    groupInput: '',
    toast: '',
    toastTimer: null,
    currentCtx: '',

    async init() {
      await this.loadContexts();
      await this.loadFiles();
      this.startSSE();

      // Cmd+K / Ctrl+K quick search
      document.addEventListener('keydown', (e) => {
        if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
          e.preventDefault();
          this.showSearch = true;
          this.$nextTick(() => this.$refs.quickInput?.focus());
        }
      });
    },

    async loadContexts() {
      const params = new URLSearchParams();
      if (this.search) params.set('q', this.search);
      if (this.filterGroup) params.set('group', this.filterGroup);
      if (this.pinnedOnly) params.set('pinned', '1');

      const res = await fetch('/api/contexts?' + params);
      if (!res.ok) return;
      const data = await res.json();
      this.contexts = data.contexts || [];
      this.currentCtx = this.contexts.find(c => c.current)?.name || '';

      // Build group list from contexts.
      const gs = new Set();
      this.contexts.forEach(c => { if (c.group) gs.add(c.group); });
      this.groupList = [...gs].sort();
    },

    get pinned() {
      return this.contexts.filter(c => c.pinned);
    },

    get groupList() {
      const gs = new Set();
      this.contexts.forEach(c => { if (c.group) gs.add(c.group); });
      return [...gs].sort();
    },

    async loadFiles() {
      const res = await fetch('/api/files');
      if (!res.ok) return;
      const data = await res.json();
      this.files = data.files || [];
    },

    openDetail(c) {
      this.selected = c;
      this.groupInput = c.group || '';
    },

    detailFields() {
      if (!this.selected) return [];
      const c = this.selected;
      const fields = [
        ['Cluster', c.cluster],
        ['User', c.user],
        ['Namespace', c.namespace || '—'],
        ['Server', c.server],
        ['Source', c.source],
        ['Group', c.group || '—'],
        ['Color', c.color || '—'],
        ['Cloud', c.cloudProvider ? `${c.cloudProvider}${c.cloudRegion ? ' / ' + c.cloudRegion : ''}` : '—'],
        ['Last used', c.lastUsed ? this.fmtDate(c.lastUsed) : '—'],
      ];
      if (c.labels && Object.keys(c.labels).length > 0) {
        fields.push(['Labels', Object.entries(c.labels).map(([k,v]) => `${k}=${v}`).join(', ')]);
      }
      return fields;
    },

    async switchCtx(name) {
      const fd = new FormData();
      fd.append('name', name);
      const res = await fetch('/api/contexts/use', { method: 'POST', body: fd });
      if (!res.ok) {
        const err = await res.json();
        this.showToast('Error: ' + err.error);
        return;
      }
      this.showToast(`Switched to "${name}"`);
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
      this.showToast('Group updated');
      await this.loadContexts();
      this.selected = this.contexts.find(c => c.name === this.selected.name) || null;
    },

    async deleteCtx(name) {
      if (!confirm(`Delete context "${name}"?`)) return;
      const fd = new FormData();
      fd.append('name', name);
      const res = await fetch('/api/contexts/delete', { method: 'POST', body: fd });
      if (!res.ok) {
        const err = await res.json();
        this.showToast('Error: ' + err.error);
        return;
      }
      this.showToast(`Deleted "${name}"`);
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
        const err = await res.json();
        this.showToast('Error: ' + err.error);
        return;
      }
      this.showToast(`Renamed to "${newName}"`);
      await this.loadContexts();
      this.selected = this.contexts.find(c => c.name === newName) || null;
    },

    exportCtx(name) {
      window.location.href = `/api/contexts/export?name=${encodeURIComponent(name)}`;
    },

    filterQuick() {
      const q = this.quickSearch.toLowerCase();
      if (!q) {
        this.quickResults = this.contexts.slice(0, 10);
        return;
      }
      this.quickResults = this.contexts.filter(c =>
        c.name.toLowerCase().includes(q) ||
        c.cluster.toLowerCase().includes(q)
      ).slice(0, 10);
    },

    startSSE() {
      const es = new EventSource('/events');
      es.onmessage = async (e) => {
        if (e.data && !e.data.startsWith(':')) {
          await this.loadContexts();
          await this.loadFiles();
        }
      };
      es.onerror = () => {
        setTimeout(() => this.startSSE(), 5000);
        es.close();
      };
    },

    fmtDate(iso) {
      try {
        return new Date(iso).toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' });
      } catch {
        return iso;
      }
    },

    shortPath(p) {
      const home = p.startsWith('/Users/') || p.startsWith('/home/');
      if (home) {
        const parts = p.split('/');
        return '~/' + parts.slice(3).join('/');
      }
      const parts = p.split('/');
      return parts.length > 2 ? '…/' + parts.slice(-2).join('/') : p;
    },

    showToast(msg) {
      this.toast = msg;
      clearTimeout(this.toastTimer);
      this.toastTimer = setTimeout(() => { this.toast = ''; }, 3000);
    },
  };
}
