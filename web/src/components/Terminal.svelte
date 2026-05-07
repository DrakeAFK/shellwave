<script>
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { RefreshCw } from 'lucide-svelte';
  import '@xterm/xterm/css/xterm.css';

  export let host = '';
  export let user = '';
  export let pass = '';
  export let port = 22;
  export let deviceId = '';

  let terminalContainer;
  let term;
  let socket;
  let fitAddon;
  let dataDisposable;
  let resizeTimer;
  let connectTimer;
  let connectionState = 'idle';
  let connectionError = '';
  let lastEvent = '';
  let hostKeyPrompt = null;
  let trustBusy = false;

  onMount(() => {
    term = new Terminal({
      cursorBlink: true,
      fontFamily: '"JetBrains Mono", monospace',
      fontSize: 14,
      theme: {
        background: '#00000000', // Transparent to show parent background
        foreground: '#f0f0f0',
        cursor: '#39ff14',
      }
    });

    fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalContainer);
    fitAndSendResize();

    window.addEventListener('resize', handleResize);

    connect();
  });

  function setConnectionState(state, message = '') {
    connectionState = state;
    connectionError = message;
  }

  function connect() {
    if (!host || !user) {
      setConnectionState('error', 'Host and user are required.');
      return;
    }
    if (!pass) {
      setConnectionState('error', 'Password is required.');
      return;
    }

    if (socket && socket.readyState !== WebSocket.CLOSED) {
      socket.close();
    }
    if (dataDisposable) {
      dataDisposable.dispose();
      dataDisposable = null;
    }
    clearTimeout(connectTimer);

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/terminal`;
    
    setConnectionState('connecting');
    lastEvent = `Connecting to ${user}@${host}:${port || 22}`;
    connectTimer = setTimeout(() => {
      if (connectionState === 'connecting') {
        setConnectionState('error', 'SSH connection timed out.');
        socket?.close();
      }
    }, 18000);
    
    socket = new WebSocket(wsUrl);

    socket.onopen = () => {
      const dimensions = getDimensions();
      socket.send(JSON.stringify({
        type: 'connect',
        deviceId,
        host,
        user,
        port: Number(port) || 22,
        cols: dimensions.cols,
        rows: dimensions.rows,
        auth: authPayload()
      }));
    };

    socket.onmessage = (event) => {
      handleMessage(event.data);
    };

    socket.onclose = () => {
      if (connectionState !== 'error') {
        setConnectionState('disconnected');
      }
      lastEvent = 'Disconnected';
    };

    socket.onerror = () => {
      setConnectionState('error', 'WebSocket connection failed.');
    };

    dataDisposable = term.onData((data) => {
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'input', data }));
      }
    });
  }

  function reconnect() {
    setConnectionState('idle');
    hostKeyPrompt = null;
    term?.clear();
    connect();
  }

  function authPayload() {
    return {
      type: 'password',
      password: pass || ''
    };
  }

  function handleMessage(data) {
    let message;
    try {
      message = JSON.parse(data);
    } catch {
      setConnectionState('error', 'Received an invalid terminal message.');
      return;
    }

    if (message.type === 'output') {
      term.write(message.data || '');
      return;
    }
    if (message.type === 'status') {
      setConnectionState(message.state || 'idle');
      if (message.state === 'connected' || message.state === 'error' || message.state === 'disconnected') {
        clearTimeout(connectTimer);
      }
      if (message.state === 'connected') {
        lastEvent = 'Connected';
      }
      return;
    }
    if (message.type === 'error') {
      clearTimeout(connectTimer);
      setConnectionState('error', message.message || 'Connection failed.');
      lastEvent = message.message || 'Connection failed.';
      hostKeyPrompt = message.hostKey || null;
      return;
    }
    if (message.type === 'exit') {
      const code = typeof message.code === 'number' ? message.code : 0;
      lastEvent = `Session exited with code ${code}`;
      return;
    }
  }

  async function trustHostKey() {
    if (!hostKeyPrompt) return;
    trustBusy = true;
    connectionError = '';
    try {
      const response = await fetch('/api/known-hosts/trust', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(hostKeyPrompt)
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) {
        throw new Error(data?.error?.message || `Trust failed with ${response.status}`);
      }
      hostKeyPrompt = null;
      reconnect();
    } catch (error) {
      connectionError = error.message;
    } finally {
      trustBusy = false;
    }
  }

  function getDimensions() {
    return {
      cols: term?.cols || 80,
      rows: term?.rows || 24
    };
  }

  function fitAndSendResize() {
    if (!fitAddon || !term) return;
    fitAddon.fit();
    const dimensions = getDimensions();
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'resize', cols: dimensions.cols, rows: dimensions.rows }));
    }
  }

  function handleResize() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(fitAndSendResize, 80);
  }

  onDestroy(() => {
    window.removeEventListener('resize', handleResize);
    clearTimeout(resizeTimer);
    clearTimeout(connectTimer);
    if (dataDisposable) dataDisposable.dispose();
    if (socket) socket.close();
    if (term) term.dispose();
  });
</script>

<div class="w-full h-full bg-black/40 rounded-lg border border-white/5 overflow-hidden flex flex-col">
  <div class="h-8 shrink-0 border-b border-white/5 px-3 flex items-center justify-between gap-4 text-[10px] uppercase tracking-widest text-muted-foreground">
    <div class="min-w-0 truncate">{lastEvent || 'Idle'}</div>
    <div class="flex items-center gap-2 shrink-0">
      <span class="h-1.5 w-1.5 rounded-full {connectionState === 'connected' ? 'bg-accent' : connectionState === 'connecting' ? 'bg-yellow-400' : connectionState === 'error' ? 'bg-red-500' : 'bg-muted-foreground'}"></span>
      <span>{connectionState}</span>
      <button on:click={reconnect} class="ml-2 rounded border border-border bg-black/60 px-2 py-1 hover:text-accent" title="Reconnect">
      <RefreshCw size={12} />
      </button>
    </div>
  </div>
  <div class="relative flex-1 min-h-0 p-2">
    {#if connectionError}
      <div class="absolute left-3 right-3 top-3 z-10 rounded-md border border-red-500/30 bg-red-950/90 px-3 py-2 text-xs text-red-100 shadow-xl">
        <div>{connectionError}</div>
        {#if hostKeyPrompt}
          <div class="mt-2 grid gap-1 text-[11px] text-red-100/80">
            <div>Host: <span class="font-mono text-red-50">{hostKeyPrompt.host}:{hostKeyPrompt.port}</span></div>
            <div>Key: <span class="font-mono text-red-50">{hostKeyPrompt.keyType}</span></div>
            <div>Fingerprint: <span class="font-mono text-red-50 break-all">{hostKeyPrompt.fingerprintSha256}</span></div>
          </div>
          <button on:click={trustHostKey} disabled={trustBusy} class="mt-3 rounded-md border border-red-300/30 bg-red-100 px-3 py-1.5 text-[11px] font-bold text-red-950 disabled:opacity-60">
            {trustBusy ? 'Trusting...' : 'Trust Fingerprint and Reconnect'}
          </button>
        {/if}
      </div>
    {/if}
    <div bind:this={terminalContainer} class="w-full h-full"></div>
  </div>
</div>
