<script>
  import { onMount } from 'svelte';
  import {
    Terminal as TerminalIcon,
    Activity,
    Plus,
    Settings,
    ChevronRight,
    Server,
    Wifi,
    X,
    RefreshCw,
    Play,
    Database,
    Shield,
    Monitor,
    Search,
    Pencil,
    Trash2,
    Check,
    Star,
    Square,
    CheckSquare
  } from 'lucide-svelte';
  import Terminal from './components/Terminal.svelte';

  let activeTab = 'overview';
  let selectedDevice = null;
  let showAddModal = false;
  let showEditModal = false;
  let showImportModal = false;
  let devices = [];
  let visibleDevices = [];
  let loadingDevices = true;
  let apiError = '';
  let authLoading = true;
  let authStatus = { setupRequired: false, authenticated: false };
  let authPassword = '';
  let authError = '';
  let authBusy = false;
  let toastTimer;
  let tailscaleStatus = null;
  let importingTailnet = false;
  let importSelections = {};
  let importUsers = {};
  let importDefaultUser = 'root';
  let importDefaultAuthMode = 'password';
  let importError = '';
  let deviceSearch = '';
  let deviceFilter = 'all';
  let sessionPasswords = {};
  let newDevice = { name: '', host: '', user: 'root', password: '', port: 22, authMode: 'password', keyPath: '' };
  let commandText = 'uname -a && uptime';
  let commandPassword = '';
  let commandRunning = false;
  let commandResult = null;
  let overview = null;
  let overviewDeviceID = '';
  let overviewProbing = false;
  let editDevice = { id: '', name: '', host: '', user: 'root', port: 22, authMode: 'password', keyPath: '', favorite: false, notes: '' };
  let quickSSHUser = '';
  let quickSSHDeviceID = '';
  let quickSSHSaving = false;

  const commandTemplates = [
    { name: 'System info', command: 'uname -a && uptime' },
    { name: 'Disk usage', command: 'df -h /' },
    { name: 'Memory usage', command: 'free -m || vm_stat' },
    { name: 'Listening ports', command: 'ss -tulpn || netstat -tulpn' },
    { name: 'Tailscale status', command: 'tailscale status' }
  ];

  onMount(() => {
    loadAuthStatus();
  });

  $: if (selectedDevice && selectedDevice.id !== overviewDeviceID) {
    loadOverview(selectedDevice.id);
  }

  $: if (selectedDevice && selectedDevice.id !== quickSSHDeviceID) {
    quickSSHDeviceID = selectedDevice.id;
    quickSSHUser = selectedDevice.user || 'root';
  }

  $: visibleDevices = filterDevices(devices, deviceSearch, deviceFilter);

  async function apiFetch(path, options = {}) {
    const response = await fetch(path, {
      headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
      credentials: 'same-origin',
      ...options
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      if (response.status === 401 && !path.startsWith('/api/auth/')) {
        authStatus = {
          setupRequired: data?.error?.code === 'setup_required',
          authenticated: false
        };
      }
      throw new Error(data?.error?.message || `Request failed with ${response.status}`);
    }
    return data;
  }

  async function loadAuthStatus() {
    authLoading = true;
    authError = '';
    try {
      authStatus = await apiFetch('/api/auth/status');
      if (authStatus.authenticated) {
        await Promise.all([loadDevices(), loadTailscaleStatus()]);
      }
    } catch (error) {
      authError = error.message;
    } finally {
      authLoading = false;
    }
  }

  async function submitAuth() {
    if (!authPassword || authBusy) return;
    authBusy = true;
    authError = '';
    try {
      await apiFetch(authStatus.setupRequired ? '/api/auth/setup' : '/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ password: authPassword })
      });
      authPassword = '';
      authStatus = { setupRequired: false, authenticated: true };
      await Promise.all([loadDevices(), loadTailscaleStatus()]);
    } catch (error) {
      authError = error.message;
    } finally {
      authBusy = false;
    }
  }

  async function logout() {
    try {
      await apiFetch('/api/auth/logout', { method: 'POST', body: '{}' });
    } catch {
      // Session may already be gone; the local UI reset is still correct.
    }
    selectedDevice = null;
    devices = [];
    authPassword = '';
    authStatus = { setupRequired: false, authenticated: false };
  }

  async function loadDevices() {
    loadingDevices = true;
    apiError = '';
    try {
      const data = await apiFetch('/api/devices');
      devices = data.devices || [];
      if (selectedDevice) {
        selectedDevice = devices.find((device) => device.id === selectedDevice.id) || null;
        if (selectedDevice) {
          quickSSHUser = selectedDevice.user || 'root';
        }
      }
    } catch (error) {
      apiError = error.message;
    } finally {
      loadingDevices = false;
    }
  }

  function showToast(message) {
    apiError = message;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      apiError = '';
    }, 2400);
  }

  async function loadTailscaleStatus() {
    try {
      tailscaleStatus = await apiFetch('/api/tailscale/status');
      return tailscaleStatus;
    } catch (error) {
      tailscaleStatus = { available: false, message: error.message, devices: [] };
      return tailscaleStatus;
    }
  }

  async function openTailnetImport() {
    showImportModal = true;
    importError = '';
    const status = await loadTailscaleStatus();
    initializeImportState(status);
  }

  function initializeImportState(status = tailscaleStatus) {
    const discovered = status?.devices || [];
    const existingIDs = new Set(devices.map((device) => device.id));
    importSelections = Object.fromEntries(discovered.map((device) => [device.id, !existingIDs.has(device.id)]));
    importUsers = Object.fromEntries(discovered.map((device) => [device.id, device.user || importDefaultUser || 'root']));
    if (discovered.length > 0 && !Object.values(importSelections).some(Boolean)) {
      importSelections = Object.fromEntries(discovered.map((device) => [device.id, true]));
    }
  }

  function updateImportDefaultUser(value) {
    const previous = importDefaultUser;
    importDefaultUser = value;
    importUsers = Object.fromEntries((tailscaleStatus?.devices || []).map((device) => {
      const current = importUsers[device.id];
      return [device.id, !current || current === previous ? value : current];
    }));
  }

  function toggleImportSelection(id) {
    importSelections = { ...importSelections, [id]: !importSelections[id] };
  }

  function setAllImportSelections(value) {
    importSelections = Object.fromEntries((tailscaleStatus?.devices || []).map((device) => [device.id, value]));
  }

  $: selectedImportCount = Object.values(importSelections).filter(Boolean).length;

  function isImported(device) {
    return devices.some((item) => item.id === device.id);
  }

  async function importTailnet() {
    importingTailnet = true;
    importError = '';
    try {
      const selected = (tailscaleStatus?.devices || []).filter((device) => importSelections[device.id]);
      const data = await apiFetch('/api/tailscale/import', {
        method: 'POST',
        body: JSON.stringify({
          defaultUser: importDefaultUser,
          defaultAuthMode: importDefaultAuthMode,
          devices: selected.map((device) => ({
            id: device.id,
            user: importUsers[device.id] || importDefaultUser || 'root',
            port: device.port || 22,
            authMode: importDefaultAuthMode
          }))
        })
      });
      devices = data.devices || [];
      if (selectedDevice) {
        selectedDevice = devices.find((device) => device.id === selectedDevice.id) || selectedDevice;
      }
      showImportModal = false;
      showToast(`Imported ${data.imported || selected.length} tailnet device${(data.imported || selected.length) === 1 ? '' : 's'}.`);
    } catch (error) {
      importError = error.message;
    } finally {
      importingTailnet = false;
    }
  }

  async function loadOverview(id) {
    overviewDeviceID = id;
    overview = null;
    try {
      const data = await apiFetch(`/api/devices/${id}/overview`);
      overview = data.overview;
    } catch (error) {
      overview = { error: error.message };
    }
  }

  function selectDevice(device) {
    selectedDevice = device;
    activeTab = 'overview';
    commandResult = null;
    commandPassword = sessionPasswords[device.id] || '';
  }

  async function addDevice() {
    apiError = '';
    try {
      const data = await apiFetch('/api/devices', {
        method: 'POST',
        body: JSON.stringify(newDevice)
      });
      const device = data.device;
      devices = [...devices.filter((item) => item.id !== device.id), device];
      if (newDevice.password) {
        sessionPasswords = { ...sessionPasswords, [device.id]: newDevice.password };
      }
      selectedDevice = device;
      commandPassword = newDevice.password;
      activeTab = 'terminal';
      showAddModal = false;
      newDevice = { name: '', host: '', user: 'root', password: '', port: 22, authMode: 'password', keyPath: '' };
    } catch (error) {
      apiError = error.message;
    }
  }

  async function testSelectedDevice() {
    if (!selectedDevice) return;
    apiError = '';
    try {
      const data = await apiFetch(`/api/devices/${selectedDevice.id}/test`, {
        method: 'POST',
        body: JSON.stringify({ auth: authPayload(selectedDevice, commandPassword || sessionPasswords[selectedDevice.id] || '') })
      });
      showToast(data?.result?.message || 'Connection test completed.');
    } catch (error) {
      apiError = error.message;
    }
  }

  function openEditDevice() {
    if (!selectedDevice) return;
    editDevice = {
      id: selectedDevice.id,
      name: selectedDevice.name,
      host: selectedDevice.host || deviceHost(selectedDevice),
      user: selectedDevice.user,
      port: selectedDevice.port || 22,
      authMode: selectedDevice.authMode || 'password',
      keyPath: selectedDevice.keyPath || '',
      favorite: !!selectedDevice.favorite,
      notes: selectedDevice.notes || ''
    };
    showEditModal = true;
  }

  async function saveDeviceEdits() {
    apiError = '';
    try {
      const data = await apiFetch(`/api/devices/${editDevice.id}`, {
        method: 'PATCH',
        body: JSON.stringify(editDevice)
      });
      selectedDevice = data.device;
      devices = devices.map((device) => device.id === selectedDevice.id ? selectedDevice : device);
      quickSSHUser = selectedDevice.user || 'root';
      showEditModal = false;
      overviewDeviceID = '';
      showToast('Device updated.');
    } catch (error) {
      apiError = error.message;
    }
  }

  async function saveQuickSSHUser() {
    if (!selectedDevice || quickSSHSaving) return;
    const user = quickSSHUser.trim();
    if (!user) {
      quickSSHUser = selectedDevice.user || 'root';
      return;
    }
    if (user === selectedDevice.user) return;
    quickSSHSaving = true;
    apiError = '';
    try {
      const data = await apiFetch(`/api/devices/${selectedDevice.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          id: selectedDevice.id,
          name: selectedDevice.name,
          host: selectedDevice.host || deviceHost(selectedDevice),
          user,
          port: selectedDevice.port || 22,
          authMode: selectedDevice.authMode || 'password',
          keyPath: selectedDevice.keyPath || '',
          favorite: !!selectedDevice.favorite,
          notes: selectedDevice.notes || ''
        })
      });
      selectedDevice = data.device;
      devices = devices.map((device) => device.id === selectedDevice.id ? selectedDevice : device);
      quickSSHUser = selectedDevice.user || 'root';
      overviewDeviceID = '';
      showToast('SSH user saved.');
    } catch (error) {
      apiError = error.message;
      quickSSHUser = selectedDevice.user || 'root';
    } finally {
      quickSSHSaving = false;
    }
  }

  function handleQuickSSHUserKeydown(event) {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveQuickSSHUser();
    }
    if (event.key === 'Escape') {
      quickSSHUser = selectedDevice?.user || 'root';
      event.currentTarget.blur();
    }
  }

  async function deleteSelectedDevice() {
    if (!selectedDevice || selectedDevice.source === 'tailscale') return;
    if (!confirm(`Delete ${selectedDevice.name}?`)) return;
    try {
      await apiFetch(`/api/devices/${selectedDevice.id}`, { method: 'DELETE' });
      devices = devices.filter((device) => device.id !== selectedDevice.id);
      selectedDevice = null;
      showToast('Device deleted.');
    } catch (error) {
      apiError = error.message;
    }
  }

  async function toggleFavorite(device) {
    try {
      const data = await apiFetch(`/api/devices/${device.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          id: device.id,
          name: device.name,
          host: device.host || deviceHost(device),
          user: device.user,
          port: device.port || 22,
          authMode: device.authMode || 'password',
          keyPath: device.keyPath || '',
          favorite: !device.favorite,
          notes: device.notes || ''
        })
      });
      devices = devices.map((item) => item.id === data.device.id ? data.device : item);
      if (selectedDevice?.id === data.device.id) {
        selectedDevice = data.device;
      }
    } catch (error) {
      apiError = error.message;
    }
  }

  function saveSelectedDevicePassword() {
    if (!selectedDevice) return;
    commandPassword = sessionPasswords[selectedDevice.id] || '';
    showToast('Session password ready.');
  }

  async function probeSelectedDevice() {
    if (!selectedDevice) return;
    overviewProbing = true;
    apiError = '';
    try {
      const data = await apiFetch(`/api/devices/${selectedDevice.id}/overview`, {
        method: 'POST',
        body: JSON.stringify({ auth: authPayload(selectedDevice, sessionPasswords[selectedDevice.id] || commandPassword || '') })
      });
      overview = { ...(overview || {}), ...(data.overview || {}) };
      showToast('Overview refreshed.');
    } catch (error) {
      apiError = error.message;
    } finally {
      overviewProbing = false;
    }
  }

  async function runCommand() {
    if (!selectedDevice || !commandText.trim()) return;
    commandRunning = true;
    commandResult = null;
    try {
      const data = await apiFetch(`/api/devices/${selectedDevice.id}/commands`, {
        method: 'POST',
        body: JSON.stringify({ command: commandText, auth: authPayload(selectedDevice, commandPassword || sessionPasswords[selectedDevice.id] || '') })
      });
      commandResult = data.result;
    } catch (error) {
      commandResult = { exitCode: -1, stderr: error.message, stdout: '' };
    } finally {
      commandRunning = false;
    }
  }

  function terminalPassword(device) {
    return (device.authMode || 'password') === 'password' ? (sessionPasswords[device.id] || '') : '';
  }

  function deviceHost(device) {
    return device.host || device.magicDns || device.tailscaleIp;
  }

  function authPayload(device, password = '') {
    const type = device?.authMode || (password ? 'password' : 'agent');
    return {
      type,
      password: type === 'password' ? password : '',
      keyPath: type === 'key' ? (device?.keyPath || '') : '',
      useAgent: type === 'agent'
    };
  }

  function filterDevices(items, query, filter) {
    const normalizedQuery = query.trim().toLowerCase();
    return items.filter((device) => {
      if (filter === 'online' && !device.online) return false;
      if (filter === 'offline' && device.online) return false;
      if (filter === 'tailnet' && device.source !== 'tailscale') return false;
      if (filter === 'manual' && device.source !== 'manual') return false;
      if (filter === 'favorites' && !device.favorite) return false;
      if (!normalizedQuery) return true;
      const haystack = [
        device.name,
        deviceHost(device),
        device.host,
        device.magicDns,
        device.tailscaleIp,
        device.user,
        device.source,
        device.authMode,
        device.os,
        ...(device.tags || [])
      ].filter(Boolean).join(' ').toLowerCase();
      return haystack.includes(normalizedQuery);
    });
  }
</script>

{#if authLoading}
  <div class="flex h-screen items-center justify-center bg-background text-foreground">
    <div class="text-xs uppercase tracking-widest text-muted-foreground">Loading ShellWave</div>
  </div>
{:else if !authStatus.authenticated}
  <div class="flex h-screen items-center justify-center bg-background px-4 text-foreground">
    <div class="w-full max-w-sm rounded-2xl border border-border bg-muted/25 p-8 shadow-2xl">
      <div class="mb-6 flex items-center gap-3">
        <div class="w-9 h-9 bg-accent rounded flex items-center justify-center">
          <TerminalIcon size={21} class="text-black" />
        </div>
        <div>
          <h1 class="font-bold tracking-tighter text-xl">SHELL<span class="text-accent">WAVE</span></h1>
          <p class="text-xs text-muted-foreground">{authStatus.setupRequired ? 'Create admin access' : 'Admin login'}</p>
        </div>
      </div>
      <form class="space-y-4" on:submit|preventDefault={submitAuth}>
        <div class="space-y-1.5">
          <label for="admin-password" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Admin Password</label>
          <input id="admin-password" bind:value={authPassword} type="password" autocomplete={authStatus.setupRequired ? 'new-password' : 'current-password'} class="w-full bg-background border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
        </div>
        {#if authError}
          <div class="rounded-md border border-red-500/30 bg-red-950/60 px-3 py-2 text-xs text-red-100">{authError}</div>
        {/if}
        <button type="submit" disabled={authBusy || !authPassword} class="w-full bg-accent text-black font-bold py-3 rounded-xl transition-transform active:scale-[0.98] disabled:opacity-50">
          {authBusy ? 'Working' : authStatus.setupRequired ? 'Create Password' : 'Log In'}
        </button>
      </form>
    </div>
  </div>
{:else}
<div class="flex h-screen bg-background text-foreground overflow-hidden">
  <aside class="w-72 border-r border-border flex flex-col bg-background/95">
    <div class="p-6 border-b border-border flex items-center gap-3">
      <div class="w-8 h-8 bg-accent rounded flex items-center justify-center">
        <TerminalIcon size={20} class="text-black" />
      </div>
      <h1 class="font-bold tracking-tighter text-xl">SHELL<span class="text-accent">WAVE</span></h1>
    </div>

    <div class="p-4 border-b border-border space-y-3">
      <button on:click={() => showAddModal = true} class="w-full bg-accent text-black font-bold px-4 py-2 rounded-md transition-transform active:scale-95 flex items-center justify-center gap-2 text-sm">
        <Plus size={16} /> Add Manual Device
      </button>
      <button on:click={openTailnetImport} disabled={importingTailnet} class="w-full bg-muted border border-border px-4 py-2 rounded-md hover:bg-muted/70 transition-colors flex items-center justify-center gap-2 text-sm disabled:opacity-50">
        <RefreshCw size={16} class={importingTailnet ? 'animate-spin' : ''} /> Import Tailnet
      </button>
      {#if tailscaleStatus && !tailscaleStatus.available}
        <p class="text-[11px] leading-relaxed text-muted-foreground">{tailscaleStatus.message}</p>
      {/if}
    </div>

    <nav class="flex-1 overflow-y-auto p-4 space-y-4">
      <div class="flex items-center justify-between px-2">
        <h2 class="text-[10px] uppercase tracking-widest text-muted-foreground font-bold">Devices</h2>
        <button on:click={loadDevices} class="hover:text-accent transition-colors" aria-label="Refresh devices">
          <RefreshCw size={14} />
        </button>
      </div>
      <div class="space-y-2">
        <div class="relative">
          <Search size={13} class="absolute left-2.5 top-2.5 text-muted-foreground" />
          <input bind:value={deviceSearch} type="search" placeholder="Search devices" class="w-full bg-muted/50 border border-border rounded-md pl-8 pr-3 py-2 text-xs focus:outline-none focus:border-accent" />
        </div>
        <div class="grid grid-cols-3 gap-1">
          {#each ['all', 'online', 'offline', 'tailnet', 'manual', 'favorites'] as filter}
            <button on:click={() => deviceFilter = filter} class="rounded border border-border px-2 py-1 text-[10px] uppercase transition-colors {deviceFilter === filter ? 'bg-accent text-black font-bold' : 'bg-muted/30 text-muted-foreground hover:text-foreground'}">
              {filter === 'tailnet' ? 'tail' : filter}
            </button>
          {/each}
        </div>
      </div>

      {#if loadingDevices}
        <p class="px-2 text-xs text-muted-foreground">Loading devices...</p>
      {:else if devices.length === 0}
        <div class="mx-2 rounded-lg border border-border bg-muted/30 p-4 text-xs text-muted-foreground leading-relaxed">
          Add a server manually or import machines from your tailnet.
        </div>
      {:else if visibleDevices.length === 0}
        <div class="mx-2 rounded-lg border border-border bg-muted/30 p-4 text-xs text-muted-foreground leading-relaxed">
          No devices match the current search and filter.
        </div>
      {:else}
        <div class="space-y-1">
          {#each visibleDevices as device (device.id)}
            <div class="group flex items-stretch gap-1 rounded-md {selectedDevice?.id === device.id ? 'bg-muted text-accent' : 'hover:bg-muted/50'}">
              <button
                on:click={() => selectDevice(device)}
                class="min-w-0 flex-1 flex items-center gap-3 px-3 py-2 rounded-md transition-all"
              >
                <div class="relative shrink-0">
                  <Server size={16} />
                  <div class="absolute -bottom-0.5 -right-0.5 w-2 h-2 rounded-full border border-background {device.online ? 'bg-accent' : 'bg-red-500'}" />
                </div>
                <div class="flex-1 text-left min-w-0">
                  <div class="text-sm font-medium truncate">{device.name}</div>
                  <div class="text-[10px] opacity-60 truncate">{deviceHost(device)}</div>
                  <div class="mt-1 flex flex-wrap gap-1">
                    <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 opacity-70">{device.source}</span>
                    <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 opacity-70">{device.authMode || 'password'}</span>
                    {#if device.os}
                      <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 opacity-70">{device.os}</span>
                    {/if}
                  </div>
                </div>
                <ChevronRight size={14} class="opacity-0 group-hover:opacity-100 transition-opacity shrink-0" />
              </button>
              <button on:click={() => toggleFavorite(device)} class="w-8 shrink-0 rounded-md text-muted-foreground hover:text-accent" aria-label={device.favorite ? 'Unfavorite device' : 'Favorite device'}>
                <Star size={14} class={device.favorite ? 'fill-current text-accent' : ''} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </nav>

    <div class="p-4 border-t border-border">
      <button on:click={logout} class="w-full flex items-center gap-3 px-3 py-2 hover:bg-muted rounded-md transition-colors text-sm opacity-60 hover:opacity-100">
        <Settings size={16} />
        <span>Sign Out</span>
      </button>
    </div>
  </aside>

  <main class="flex-1 flex flex-col min-w-0 relative">
    {#if apiError}
      <div class="absolute right-6 bottom-6 z-50 max-w-md rounded-md border border-border bg-muted px-4 py-2 text-xs shadow-xl">
        {apiError}
      </div>
    {/if}

    {#if selectedDevice}
      <header class="h-16 border-b border-border flex items-center justify-between px-8 shrink-0">
        <div class="flex items-center gap-4 min-w-0">
          <div class="flex items-center gap-2 min-w-0">
            <span class="text-accent font-bold tracking-tighter text-lg uppercase truncate max-w-[260px]">{selectedDevice.name}</span>
            <span class="text-[10px] bg-muted px-2 py-0.5 rounded-full border border-border opacity-70 shrink-0">{deviceHost(selectedDevice)}</span>
          </div>
          <div class="hidden lg:flex items-center gap-2 rounded-md border border-border bg-muted/60 px-2 py-1 shrink-0">
            <span class="text-[10px] uppercase tracking-widest text-muted-foreground">SSH</span>
            <input
              bind:value={quickSSHUser}
              on:blur={saveQuickSSHUser}
              on:keydown={handleQuickSSHUserKeydown}
              aria-label="SSH user"
              class="w-24 bg-background/70 border border-border rounded px-2 py-1 text-xs font-mono focus:outline-none focus:border-accent"
            />
            <button on:mousedown|preventDefault on:click={saveQuickSSHUser} disabled={quickSSHSaving || quickSSHUser.trim() === selectedDevice.user} class="rounded border border-border bg-background p-1 hover:text-accent disabled:opacity-40" aria-label="Save SSH user">
              <Check size={12} />
            </button>
          </div>
        </div>

        <div class="flex bg-muted p-1 rounded-lg border border-border shrink-0">
          <button on:click={() => activeTab = 'overview'} class="px-4 py-1.5 rounded-md text-xs font-medium transition-all flex items-center gap-2 {activeTab === 'overview' ? 'bg-background text-accent shadow-sm' : 'hover:bg-background/50 opacity-60'}">
            <Activity size={14} /> Overview
          </button>
          <button on:click={() => activeTab = 'terminal'} class="px-4 py-1.5 rounded-md text-xs font-medium transition-all flex items-center gap-2 {activeTab === 'terminal' ? 'bg-background text-accent shadow-sm' : 'hover:bg-background/50 opacity-60'}">
            <TerminalIcon size={14} /> Terminal
          </button>
          <button on:click={() => activeTab = 'commands'} class="px-4 py-1.5 rounded-md text-xs font-medium transition-all flex items-center gap-2 {activeTab === 'commands' ? 'bg-background text-accent shadow-sm' : 'hover:bg-background/50 opacity-60'}">
            <Play size={14} /> Commands
          </button>
        </div>

        <div class="hidden md:flex items-center gap-4 text-right">
          <div class="flex flex-col items-end">
            <span class="text-[10px] font-bold uppercase text-muted-foreground">Source</span>
            <span class="text-xs capitalize">{selectedDevice.source} / {selectedDevice.authMode || 'password'}</span>
          </div>
          <button on:click={openEditDevice} class="border border-border bg-muted p-2 rounded-md hover:text-accent transition-colors" aria-label="Edit device">
            <Pencil size={14} />
          </button>
          {#if selectedDevice.source !== 'tailscale'}
            <button on:click={deleteSelectedDevice} class="border border-border bg-muted p-2 rounded-md hover:text-red-400 transition-colors" aria-label="Delete device">
              <Trash2 size={14} />
            </button>
          {/if}
        </div>
      </header>

      <div class="flex-1 overflow-hidden p-8">
        {#if activeTab === 'terminal'}
          <div class="w-full h-full bg-black/50 border border-border rounded-xl shadow-2xl relative overflow-hidden backdrop-blur-sm">
            <div class="absolute inset-0 p-4">
              <div class="w-full h-full flex flex-col">
                <div class="flex items-center justify-between mb-4 px-2 shrink-0">
                  <div class="flex gap-1.5">
                    <div class="w-2.5 h-2.5 rounded-full bg-red-500/50" />
                    <div class="w-2.5 h-2.5 rounded-full bg-yellow-500/50" />
                    <div class="w-2.5 h-2.5 rounded-full bg-green-500/50" />
                  </div>
                  <div class="text-[10px] text-muted-foreground font-mono">ssh -t {selectedDevice.user}@{deviceHost(selectedDevice)}</div>
                </div>
                <div class="flex-1 min-h-0">
                  {#key `${selectedDevice.id}:${deviceHost(selectedDevice)}:${selectedDevice.user}:${selectedDevice.port || 22}:${selectedDevice.authMode || 'password'}:${selectedDevice.keyPath || ''}`}
                    <Terminal
                      host={deviceHost(selectedDevice)}
                      user={selectedDevice.user}
                      pass={terminalPassword(selectedDevice)}
                      port={selectedDevice.port || 22}
                      authMode={selectedDevice.authMode || 'password'}
                      keyPath={selectedDevice.keyPath || ''}
                    />
                  {/key}
                </div>
              </div>
            </div>
          </div>
        {:else if activeTab === 'overview'}
          <div class="h-full overflow-y-auto space-y-6">
            <section class="border border-border bg-muted/25 rounded-xl p-6">
              <div class="flex items-start justify-between gap-6">
                <div>
                  <div class="flex items-center gap-3 mb-2">
                    <Monitor size={22} class="text-accent" />
                    <h2 class="text-2xl font-bold tracking-tight">{selectedDevice.name}</h2>
                  </div>
                  <p class="text-sm text-muted-foreground font-mono">{deviceHost(selectedDevice)}</p>
                </div>
                <div class="flex gap-2">
                  <button on:click={probeSelectedDevice} disabled={overviewProbing} class="border border-border bg-background px-4 py-2 rounded-md text-xs hover:text-accent transition-colors disabled:opacity-50">{overviewProbing ? 'Refreshing' : 'Refresh Overview'}</button>
                  <button on:click={testSelectedDevice} class="border border-border bg-background px-4 py-2 rounded-md text-xs hover:text-accent transition-colors">Test SSH</button>
                </div>
              </div>
            </section>

            <div class="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <Database size={18} class="text-accent mb-3" />
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Source</div>
                <div class="text-sm capitalize">{selectedDevice.source}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <Wifi size={18} class="text-accent mb-3" />
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Status</div>
                <div class="text-sm">{selectedDevice.online ? 'Online' : 'Unknown'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <Shield size={18} class="text-accent mb-3" />
                <div class="text-[10px] uppercase text-muted-foreground font-bold">SSH Auth</div>
                <div class="text-sm">{selectedDevice.user} / {selectedDevice.authMode || 'password'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <Search size={18} class="text-accent mb-3" />
                <div class="text-[10px] uppercase text-muted-foreground font-bold">OS</div>
                <div class="text-sm">{overview?.os || selectedDevice.os || 'Unknown'}</div>
              </div>
            </div>

            <div class="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Hostname</div>
                <div class="text-sm mt-2">{overview?.hostname || 'Unknown'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Kernel</div>
                <div class="text-sm mt-2">{overview?.kernel || 'Unknown'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Disk</div>
                <div class="text-sm mt-2">{overview?.disk || 'Unknown'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Memory</div>
                <div class="text-sm mt-2">{overview?.memory || 'Unknown'}</div>
              </div>
            </div>

            <div class="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Uptime</div>
                <div class="text-sm mt-2">{overview?.uptime || 'Unknown'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Load Average</div>
                <div class="text-sm mt-2">{overview?.load ? `${overview.load} (${overview.cpuCount || 1} CPU)` : 'Unknown'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Docker</div>
                <div class="text-sm mt-2">{overview?.dockerInstalled === 'true' ? `${overview?.dockerContainers} running` : 'Not installed'}</div>
              </div>
              <div class="border border-border bg-muted/25 rounded-lg p-4">
                <div class="text-[10px] uppercase text-muted-foreground font-bold">Listening Ports</div>
                <div class="text-sm mt-2">{overview?.portsCount || 'Unknown'}</div>
              </div>
            </div>

            <section class="border border-border bg-muted/25 rounded-xl p-6 space-y-3">
              <h3 class="font-bold">Connection Details</h3>
              <div class="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                <div class="text-muted-foreground">Host <span class="text-foreground font-mono">{selectedDevice.host || 'Unset'}</span></div>
                <div class="text-muted-foreground">MagicDNS <span class="text-foreground font-mono">{selectedDevice.magicDns || 'Unset'}</span></div>
                <div class="text-muted-foreground">Tailscale IP <span class="text-foreground font-mono">{selectedDevice.tailscaleIp || 'Unset'}</span></div>
                <div class="text-muted-foreground">Port <span class="text-foreground font-mono">{selectedDevice.port || 22}</span></div>
              </div>
            </section>

            <section class="border border-border bg-muted/25 rounded-xl p-6 space-y-4">
              <div class="flex items-center justify-between gap-4">
                <h3 class="font-bold">SSH Authentication</h3>
                {#if (selectedDevice.authMode || 'password') === 'password'}
                  <button on:click={saveSelectedDevicePassword} class="border border-border bg-background px-4 py-2 rounded-md text-xs hover:text-accent transition-colors">Use Password</button>
                {/if}
              </div>
              {#if (selectedDevice.authMode || 'password') === 'password'}
                <div class="space-y-1.5">
                  <label for="selected-session-password" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Session Password</label>
                  <input id="selected-session-password" bind:value={sessionPasswords[selectedDevice.id]} type="password" class="w-full bg-background border border-border rounded-md px-3 py-2 text-sm focus:outline-none focus:border-accent" />
                </div>
                <p class="text-[11px] leading-relaxed text-muted-foreground">Passwords are kept in this browser session only and are sent in the first WebSocket message or command request body, never in the URL.</p>
              {:else if selectedDevice.authMode === 'key'}
                <div class="rounded-md border border-border bg-background px-3 py-2 text-xs text-muted-foreground">
                  Key path <span class="font-mono text-foreground">{selectedDevice.keyPath || 'Not configured'}</span>
                </div>
              {:else}
                <div class="rounded-md border border-border bg-background px-3 py-2 text-xs text-muted-foreground">
                  ShellWave will use the server process SSH agent through <span class="font-mono text-foreground">SSH_AUTH_SOCK</span>.
                </div>
              {/if}
            </section>
          </div>
        {:else if activeTab === 'commands'}
          <div class="h-full overflow-y-auto grid grid-cols-1 xl:grid-cols-[320px_1fr] gap-6">
            <section class="border border-border bg-muted/25 rounded-xl p-4 space-y-3">
              <h3 class="font-bold">Command Templates</h3>
              {#each commandTemplates as template}
                <button on:click={() => commandText = template.command} class="w-full text-left border border-border bg-background/60 rounded-md p-3 hover:text-accent transition-colors">
                  <div class="text-sm font-bold">{template.name}</div>
                  <div class="text-[11px] text-muted-foreground font-mono truncate">{template.command}</div>
                </button>
              {/each}
            </section>

            <section class="border border-border bg-muted/25 rounded-xl p-4 flex flex-col min-h-[520px] space-y-4">
              <div class="grid grid-cols-1 {(selectedDevice.authMode || 'password') === 'password' ? 'md:grid-cols-[1fr_220px_auto]' : 'md:grid-cols-[1fr_auto]'} gap-3">
                <input bind:value={commandText} class="bg-background border border-border rounded-md px-3 py-2 font-mono text-sm focus:outline-none focus:border-accent" />
                {#if (selectedDevice.authMode || 'password') === 'password'}
                  <input bind:value={commandPassword} type="password" placeholder="Password for this run" class="bg-background border border-border rounded-md px-3 py-2 text-sm focus:outline-none focus:border-accent" />
                {/if}
                <button on:click={runCommand} disabled={commandRunning} class="bg-accent text-black font-bold rounded-md px-4 py-2 text-sm disabled:opacity-50 flex items-center gap-2 justify-center">
                  <Play size={14} /> {commandRunning ? 'Running' : 'Run'}
                </button>
              </div>
              <pre class="flex-1 min-h-0 overflow-auto bg-black/60 border border-border rounded-lg p-4 text-xs leading-relaxed whitespace-pre-wrap">{#if commandResult}{commandResult.stdout}{#if commandResult.stderr}
--- stderr ---
{commandResult.stderr}{/if}

exit {commandResult.exitCode}{:else}Command output will appear here.{/if}</pre>
            </section>
          </div>
        {/if}
      </div>
    {:else}
      <div class="flex-1 overflow-y-auto p-8">
        <div class="mx-auto max-w-4xl space-y-6">
          <section class="border border-border bg-muted/25 rounded-2xl p-8">
            <div class="flex flex-col md:flex-row md:items-start md:justify-between gap-6">
              <div class="space-y-3">
                <div class="w-14 h-14 bg-muted rounded-xl flex items-center justify-center">
                  <Wifi size={28} class="text-muted-foreground" />
                </div>
                <div>
                  <h2 class="text-2xl font-bold tracking-tight mb-2">Set Up Your Machine Console</h2>
                  <p class="text-muted-foreground max-w-xl">
                    Import devices from your tailnet or add a host manually. ShellWave stores metadata locally in SQLite and keeps SSH passwords in the browser session only.
                  </p>
                </div>
              </div>
              <div class="flex flex-col sm:flex-row md:flex-col gap-3 min-w-[220px]">
                <button on:click={openTailnetImport} class="bg-accent text-black font-bold px-6 py-2.5 rounded-lg transition-transform active:scale-95">
                  Import Tailnet
                </button>
                <button on:click={() => showAddModal = true} class="border border-border bg-muted px-6 py-2.5 rounded-lg hover:text-accent transition-colors">
                  Add Manual Device
                </button>
              </div>
            </div>
          </section>

          <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
            <section class="border border-border bg-muted/20 rounded-xl p-5">
              <div class="text-[10px] uppercase tracking-widest text-muted-foreground font-bold">Tailscale</div>
              <div class="mt-3 text-sm">
                {#if !tailscaleStatus}
                  Checking local CLI...
                {:else if tailscaleStatus.available}
                  Active with {(tailscaleStatus.devices || []).length} device{(tailscaleStatus.devices || []).length === 1 ? '' : 's'} discovered.
                {:else}
                  {tailscaleStatus.message || 'Tailscale is not available.'}
                {/if}
              </div>
            </section>
            <section class="border border-border bg-muted/20 rounded-xl p-5">
              <div class="text-[10px] uppercase tracking-widest text-muted-foreground font-bold">Default SSH</div>
              <div class="mt-3 text-sm">Imported devices start as <span class="font-mono text-foreground">root</span> with <span class="font-mono text-foreground">password</span> auth.</div>
            </section>
            <section class="border border-border bg-muted/20 rounded-xl p-5">
              <div class="text-[10px] uppercase tracking-widest text-muted-foreground font-bold">Data</div>
              <div class="mt-3 text-sm">Local metadata lives under <span class="font-mono text-foreground">~/.config/shellwave</span> unless overridden.</div>
            </section>
          </div>
        </div>
      </div>
    {/if}
  </main>
</div>
{#if showImportModal}
  <div class="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
    <div class="bg-background border border-border w-full max-w-4xl max-h-[86vh] rounded-2xl p-6 shadow-2xl flex flex-col gap-5">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h2 class="text-xl font-bold tracking-tight">Import Tailnet Devices</h2>
          <p class="mt-1 text-xs text-muted-foreground">Select devices discovered from the local Tailscale CLI and choose the SSH user ShellWave should try first.</p>
        </div>
        <button on:click={() => showImportModal = false} class="text-muted-foreground hover:text-foreground" aria-label="Close import modal">
          <X size={20} />
        </button>
      </div>

      {#if tailscaleStatus && !tailscaleStatus.available}
        <div class="rounded-lg border border-border bg-muted/40 p-4 text-sm text-muted-foreground">
          {tailscaleStatus.message || 'Tailscale is unavailable on this host.'}
        </div>
        <div class="flex justify-end">
          <button on:click={() => { showImportModal = false; showAddModal = true; }} class="bg-accent text-black font-bold px-4 py-2 rounded-md text-sm">Add Manual Device</button>
        </div>
      {:else}
        <div class="grid grid-cols-1 md:grid-cols-[1fr_180px_160px] gap-3">
          <div class="space-y-1.5">
            <label for="import-default-user" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Default SSH User</label>
            <input id="import-default-user" value={importDefaultUser} on:input={(event) => updateImportDefaultUser(event.currentTarget.value)} class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 text-sm focus:outline-none focus:border-accent" />
          </div>
          <div class="space-y-1.5">
            <label for="import-auth-mode" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Auth Mode</label>
            <select id="import-auth-mode" bind:value={importDefaultAuthMode} class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 text-sm focus:outline-none focus:border-accent">
              <option value="password">Password</option>
              <option value="key">Key Path</option>
              <option value="agent">SSH Agent</option>
            </select>
          </div>
          <div class="flex items-end gap-2">
            <button on:click={() => setAllImportSelections(true)} class="flex-1 border border-border bg-muted px-3 py-2.5 rounded-lg text-xs hover:text-accent">All</button>
            <button on:click={() => setAllImportSelections(false)} class="flex-1 border border-border bg-muted px-3 py-2.5 rounded-lg text-xs hover:text-accent">None</button>
          </div>
        </div>

        <div class="min-h-0 overflow-auto border border-border rounded-xl">
          {#if !tailscaleStatus}
            <div class="p-6 text-sm text-muted-foreground">Loading tailnet devices...</div>
          {:else if (tailscaleStatus.devices || []).length === 0}
            <div class="p-6 text-sm text-muted-foreground">No tailnet peers were reported by Tailscale.</div>
          {:else}
            <div class="divide-y divide-border">
              {#each tailscaleStatus.devices || [] as device (device.id)}
                <div class="grid grid-cols-[auto_1fr] md:grid-cols-[auto_1fr_180px] gap-3 p-3 items-center {importSelections[device.id] ? 'bg-muted/25' : 'bg-background'}">
                  <button on:click={() => toggleImportSelection(device.id)} class="p-1 text-muted-foreground hover:text-accent" aria-label={importSelections[device.id] ? 'Deselect device' : 'Select device'}>
                    {#if importSelections[device.id]}
                      <CheckSquare size={18} />
                    {:else}
                      <Square size={18} />
                    {/if}
                  </button>
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="font-medium truncate">{device.name}</span>
                      <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 {device.online ? 'text-accent' : 'text-red-300'}">{device.online ? 'online' : 'offline'}</span>
                      {#if isImported(device)}
                        <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 text-muted-foreground">imported</span>
                      {/if}
                      {#if device.os}
                        <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 text-muted-foreground">{device.os}</span>
                      {/if}
                    </div>
                    <div class="mt-1 text-[11px] text-muted-foreground font-mono truncate">{device.magicDns || device.host}</div>
                    <div class="mt-1 text-[11px] text-muted-foreground font-mono truncate">{device.tailscaleIp}</div>
                    {#if device.tags?.length}
                      <div class="mt-2 flex flex-wrap gap-1">
                        {#each device.tags as tag}
                          <span class="text-[9px] uppercase border border-border rounded px-1.5 py-0.5 text-muted-foreground">{tag}</span>
                        {/each}
                      </div>
                    {/if}
                  </div>
                  <div class="col-span-2 md:col-span-1 space-y-1.5">
                    <label class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest" for={`import-user-${device.id}`}>SSH User</label>
                    <input id={`import-user-${device.id}`} bind:value={importUsers[device.id]} class="w-full bg-background border border-border rounded-md px-3 py-2 text-sm font-mono focus:outline-none focus:border-accent" />
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        {#if importError}
          <div class="rounded-md border border-red-500/30 bg-red-950/60 px-3 py-2 text-xs text-red-100">{importError}</div>
        {/if}

        <div class="flex items-center justify-between gap-3">
          <div class="text-xs text-muted-foreground">{selectedImportCount} selected</div>
          <div class="flex gap-2">
            <button on:click={() => showImportModal = false} class="border border-border bg-muted px-4 py-2 rounded-md text-sm hover:text-accent">Cancel</button>
            <button on:click={importTailnet} disabled={importingTailnet || selectedImportCount === 0} class="bg-accent text-black font-bold px-4 py-2 rounded-md text-sm disabled:opacity-50">
              {importingTailnet ? 'Importing' : 'Import Selected'}
            </button>
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}

{#if showAddModal}
  <div class="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
    <div class="bg-background border border-border w-full max-w-md rounded-2xl p-8 shadow-2xl space-y-6">
      <div class="flex items-center justify-between">
        <h2 class="text-xl font-bold tracking-tight">Add Manual Device</h2>
        <button on:click={() => showAddModal = false} class="text-muted-foreground hover:text-foreground" aria-label="Close modal">
          <X size={20} />
        </button>
      </div>

      <div class="space-y-4">
        <div class="space-y-1.5">
          <label for="device-name" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Device Name</label>
          <input id="device-name" bind:value={newDevice.name} type="text" placeholder="e.g. My Server" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
        </div>
        <div class="grid grid-cols-3 gap-4">
          <div class="col-span-2 space-y-1.5">
            <label for="device-host" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Host / IP</label>
            <input id="device-host" bind:value={newDevice.host} type="text" placeholder="100.x.y.z" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
          <div class="space-y-1.5">
            <label for="device-port" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Port</label>
            <input id="device-port" bind:value={newDevice.port} type="number" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
        </div>
        <div class="grid grid-cols-2 gap-4">
          <div class="space-y-1.5">
            <label for="device-user" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">User</label>
            <input id="device-user" bind:value={newDevice.user} type="text" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
          <div class="space-y-1.5">
            <label for="device-auth-mode" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Auth Mode</label>
            <select id="device-auth-mode" bind:value={newDevice.authMode} class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors">
              <option value="password">Password</option>
              <option value="key">Key Path</option>
              <option value="agent">SSH Agent</option>
            </select>
          </div>
        </div>
        {#if newDevice.authMode === 'password'}
          <div class="space-y-1.5">
            <label for="device-password" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Session Password</label>
            <input id="device-password" bind:value={newDevice.password} type="password" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
          <p class="text-[11px] leading-relaxed text-muted-foreground">Passwords are used for the current browser session and are not persisted by the backend.</p>
        {:else if newDevice.authMode === 'key'}
          <div class="space-y-1.5">
            <label for="device-key-path" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Private Key Path</label>
            <input id="device-key-path" bind:value={newDevice.keyPath} type="text" placeholder="~/.ssh/id_ed25519" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
        {:else}
          <p class="text-[11px] leading-relaxed text-muted-foreground">Agent mode uses the ShellWave server process SSH agent. Make sure <span class="font-mono">SSH_AUTH_SOCK</span> is available where the server runs.</p>
        {/if}
      </div>

      <button on:click={addDevice} class="w-full bg-accent text-black font-bold py-3 rounded-xl hover:scale-[1.02] transition-transform active:scale-[0.98]">
        Save and Open Terminal
      </button>
    </div>
  </div>
{/if}

{#if showEditModal}
  <div class="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
    <div class="bg-background border border-border w-full max-w-md rounded-2xl p-8 shadow-2xl space-y-6">
      <div class="flex items-center justify-between">
        <h2 class="text-xl font-bold tracking-tight">Edit Device</h2>
        <button on:click={() => showEditModal = false} class="text-muted-foreground hover:text-foreground" aria-label="Close modal">
          <X size={20} />
        </button>
      </div>

      <div class="space-y-4">
        <div class="space-y-1.5">
          <label for="edit-device-name" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Device Name</label>
          <input id="edit-device-name" bind:value={editDevice.name} type="text" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
        </div>
        <div class="grid grid-cols-3 gap-4">
          <div class="col-span-2 space-y-1.5">
            <label for="edit-device-host" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Host / IP</label>
            <input id="edit-device-host" bind:value={editDevice.host} type="text" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
          <div class="space-y-1.5">
            <label for="edit-device-port" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Port</label>
            <input id="edit-device-port" bind:value={editDevice.port} type="number" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
        </div>
        <div class="grid grid-cols-2 gap-4">
          <div class="space-y-1.5">
            <label for="edit-device-user" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">User</label>
            <input id="edit-device-user" bind:value={editDevice.user} type="text" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
          <div class="space-y-1.5">
            <label for="edit-device-auth-mode" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Auth Mode</label>
            <select id="edit-device-auth-mode" bind:value={editDevice.authMode} class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors">
              <option value="password">Password</option>
              <option value="key">Key Path</option>
              <option value="agent">SSH Agent</option>
            </select>
          </div>
        </div>
        {#if editDevice.authMode === 'key'}
          <div class="space-y-1.5">
            <label for="edit-device-key-path" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Private Key Path</label>
            <input id="edit-device-key-path" bind:value={editDevice.keyPath} type="text" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors" />
          </div>
        {/if}
        <label class="flex items-center gap-3 rounded-lg border border-border bg-muted px-4 py-3 text-sm">
          <input bind:checked={editDevice.favorite} type="checkbox" class="accent-lime-300" />
          <span>Favorite device</span>
        </label>
        <div class="space-y-1.5">
          <label for="edit-device-notes" class="text-[10px] uppercase font-bold text-muted-foreground tracking-widest">Notes</label>
          <textarea id="edit-device-notes" bind:value={editDevice.notes} rows="3" class="w-full bg-muted border border-border rounded-lg px-4 py-2.5 focus:outline-none focus:border-accent transition-colors"></textarea>
        </div>
      </div>

      <button on:click={saveDeviceEdits} class="w-full bg-accent text-black font-bold py-3 rounded-xl hover:scale-[1.02] transition-transform active:scale-[0.98]">
        Save Device
      </button>
    </div>
  </div>
{/if}
{/if}

<style>
  :global(body) {
    background-color: #0a0a0a;
    color: #f0f0f0;
  }
</style>
