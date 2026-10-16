// Host console for the Ether bridge. Plain DOM, no dependencies; everything
// user-provided is rendered with textContent. The admin token lives only in
// sessionStorage (this tab).
'use strict';

const $ = (selector) => document.querySelector(selector);
const TOKEN_KEY = 'ether-bridge-admin-token';
const KIND_LABELS = { youtube: 'YouTube', twitch: 'Twitch', facebook: 'Facebook', custom: 'Otro' };
const SOURCE_LABELS = { camera: 'cámara', microphone: 'micrófono', screen: 'pantalla', 'screen-audio': 'audio pantalla' };

const state = { token: '', rooms: [], selected: '', room: null, timer: 0 };

function storedToken() {
  try {
    return sessionStorage.getItem(TOKEN_KEY) || '';
  } catch {
    return '';
  }
}

function storeToken(token) {
  try {
    if (token) sessionStorage.setItem(TOKEN_KEY, token);
    else sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    /* private mode: keep it in memory only */
  }
}

class ApiError extends Error {
  constructor(status, code, message) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function api(method, path, body) {
  const response = await fetch(`/api/v1${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${state.token}`,
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 204) return null;
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401) logout();
    throw new ApiError(response.status, data.code || 'error', data.message || response.statusText);
  }
  return data;
}

function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key === 'text') node.textContent = value;
    else if (key === 'class') node.className = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else if (value !== undefined && value !== null && value !== false) node.setAttribute(key, value === true ? '' : value);
  }
  for (const child of children) node.append(child);
  return node;
}

function showError(target, error) {
  target.textContent = error ? error.message || String(error) : '';
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

async function login(token) {
  state.token = token;
  const config = await api('GET', '/config');
  storeToken(token);
  $('#dev-badge').hidden = !config.devMode;
  $('#login').hidden = true;
  $('#app').hidden = false;
  $('#logout').hidden = false;
  await refreshRooms();
  clearInterval(state.timer);
  state.timer = setInterval(tick, 2000);
}

function logout() {
  clearInterval(state.timer);
  state.token = '';
  storeToken('');
  $('#app').hidden = true;
  $('#logout').hidden = true;
  $('#login').hidden = false;
  $('#login-token').focus();
}

async function tick() {
  if (document.hidden) return;
  try {
    await refreshRooms();
  } catch {
    /* transient; the next tick retries */
  }
}

// ---------------------------------------------------------------------------
// Rooms
// ---------------------------------------------------------------------------

async function refreshRooms() {
  const { rooms } = await api('GET', '/rooms');
  state.rooms = rooms;
  renderRoomList();
  if (state.selected) {
    const room = rooms.find((r) => r.id === state.selected);
    if (room) {
      state.room = room;
      renderRoom();
      await Promise.all([refreshLayout(), refreshInvites()]);
    } else {
      selectRoom('');
    }
  }
}

function renderRoomList() {
  const list = $('#room-list');
  list.replaceChildren(
    ...state.rooms.map((room) => {
      const live = room.broadcast?.live;
      const meta = [
        room.id,
        `${room.participants.length} participante${room.participants.length === 1 ? '' : 's'}`,
        room.closed ? 'cerrada' : live ? '● en vivo' : '',
      ].filter(Boolean);
      return el('li', {}, [
        el('button', { type: 'button', 'aria-current': String(room.id === state.selected), onclick: () => selectRoom(room.id) }, [
          el('span', { text: room.title }),
          el('span', { class: 'meta', text: meta.join(' · ') }),
        ]),
      ]);
    }),
  );
  if (state.rooms.length === 0) list.append(el('li', { class: 'muted', text: 'No hay salas.' }));
}

function selectRoom(id) {
  state.selected = id;
  state.room = state.rooms.find((r) => r.id === id) || null;
  $('#room').hidden = !state.room;
  $('#empty').hidden = Boolean(state.room);
  $('#invite-result').hidden = true;
  showError($('#room-error'), null);
  renderRoomList();
  if (state.room) {
    fillSettings();
    renderRoom();
    void refreshLayout();
    void refreshInvites();
  }
}

function fillSettings() {
  const form = $('#settings-form');
  form.title.value = state.room.title;
  form.host.value = state.room.host.name;
  form.capacity.value = state.room.capacity;
  $('#layout-mode').value = state.room.layout;
}

function renderRoom() {
  const room = state.room;
  $('#room-title').textContent = room.title;
  $('#room-id').textContent = room.ephemeral ? `${room.id} (sala de desarrollo)` : room.id;
  $('#room-host').textContent = `Anfitrión: ${room.host.name}`;

  const broadcast = room.broadcast || {};
  const live = Boolean(broadcast.live);
  const status = $('#air-status');
  status.textContent = room.closed ? 'Sala cerrada' : live ? '● En vivo' : 'Fuera del aire';
  status.classList.toggle('live', live);
  $('#air-start').hidden = live || room.closed;
  $('#air-stop').hidden = !live;
  $('#air-start').disabled = !room.destinations.some((d) => d.enabled);
  $('#air-start').title = $('#air-start').disabled ? 'Añade y activa al menos un destino' : '';
  const detail = [];
  if (live && broadcast.since) detail.push(`desde ${new Date(broadcast.since).toLocaleTimeString()}`);
  if (broadcast.restarts) detail.push(`${broadcast.restarts} reinicio(s)`);
  if (broadcast.lastError) detail.push(`último error: ${broadcast.lastError}`);
  $('#air-detail').textContent = detail.join(' · ');
  if (document.activeElement !== $('#layout-mode')) $('#layout-mode').value = room.layout;

  renderParticipants(room);
  renderWaiting(room);
  renderDestinations(room);
  for (const control of document.querySelectorAll('#room button, #room input, #room select')) {
    if (control.id !== 'air-stop' && control.id !== 'invite-copy') control.disabled ||= room.closed;
  }
}

function chip(label, on) {
  return el('span', { class: `chip ${on ? 'on' : 'off'}`, text: label });
}

function renderParticipants(room) {
  $('#participant-count').textContent = `(${room.participants.length}/${room.capacity})`;
  $('#no-participants').hidden = room.participants.length > 0;
  $('#participants').replaceChildren(
    ...room.participants.map((p) =>
      el('tr', {}, [
        el('td', {}, [el('strong', { text: p.name }), p.title ? el('div', { class: 'muted small', text: p.title }) : '']),
        el('td', {}, [
          el('span', { class: 'state' }, [
            chip(p.muted ? 'mic off' : 'mic', !p.muted),
            chip(p.cameraOff ? 'cám off' : 'cám', !p.cameraOff),
            ...(p.screenSharing ? [chip('pantalla', true)] : []),
            ...(p.connected ? [] : [chip('reconectando', false)]),
          ]),
        ]),
        el('td', { class: 'small muted', text: p.published.map((s) => SOURCE_LABELS[s] || s).join(', ') || '—' }),
        el('td', {}, [
          el('input', {
            type: 'checkbox', class: 'switch', checked: p.onStage, 'aria-label': `${p.name} en escena`,
            onchange: (event) => mutate(`/rooms/${room.id}/participants/${p.id}`, 'PATCH', { onStage: event.target.checked }),
          }),
        ]),
        el('td', {}, [
          el('button', {
            type: 'button', class: 'ghost small', text: 'Retirar',
            onclick: () => {
              if (confirm(`¿Retirar a ${p.name} de la sala?`)) mutate(`/rooms/${room.id}/participants/${p.id}`, 'DELETE');
            },
          }),
        ]),
      ]),
    ),
  );
}

function renderWaiting(room) {
  if (document.activeElement !== $('#admission-mode')) $('#admission-mode').value = room.admission;
  const waiting = room.waiting || [];
  $('#waiting-box').hidden = waiting.length === 0;
  $('#no-waiting').hidden = waiting.length > 0 || room.admission !== 'approval';
  $('#waiting-count').textContent = waiting.length ? `(${waiting.length})` : '';
  $('#waiting').replaceChildren(
    ...waiting.map((w) =>
      el('tr', {}, [
        el('td', {}, [el('strong', { text: w.name }), w.title ? el('div', { class: 'muted small', text: w.title }) : '']),
        el('td', { class: 'small muted', text: new Date(w.since).toLocaleTimeString() }),
        el('td', {}, [
          w.admitted
            ? el('span', { class: 'small muted', text: 'admitido, entrando…' })
            : el('button', { type: 'button', class: 'small', text: 'Admitir', onclick: () => mutate(`/rooms/${room.id}/waiting/${w.id}/admit`, 'POST') }),
          ' ',
          w.admitted
            ? ''
            : el('button', { type: 'button', class: 'ghost small', text: 'Denegar', onclick: () => mutate(`/rooms/${room.id}/waiting/${w.id}/deny`, 'POST') }),
        ]),
      ]),
    ),
  );
}

function renderDestinations(room) {
  $('#destinations').replaceChildren(
    ...room.destinations.map((d) =>
      el('tr', {}, [
        el('td', { text: d.name }),
        el('td', { text: KIND_LABELS[d.kind] || d.kind }),
        el('td', { class: 'break small', text: d.url }),
        el('td', { class: 'small muted', text: d.streamKey || '—' }),
        el('td', {}, [
          el('input', {
            type: 'checkbox', class: 'switch', checked: d.enabled, 'aria-label': `${d.name} activo`,
            onchange: (event) => mutate(`/rooms/${room.id}/destinations/${d.id}`, 'PATCH', { enabled: event.target.checked }),
          }),
        ]),
        el('td', {}, [
          el('button', {
            type: 'button', class: 'ghost small', text: 'Quitar',
            onclick: () => {
              if (confirm(`¿Quitar el destino ${d.name}?`)) mutate(`/rooms/${room.id}/destinations/${d.id}`, 'DELETE');
            },
          }),
        ]),
      ]),
    ),
  );
  if (room.destinations.length === 0) {
    $('#destinations').append(el('tr', {}, [el('td', { colspan: '6', class: 'muted', text: 'Sin destinos: añade al menos uno para salir al aire.' })]));
  }
}

async function refreshInvites() {
  if (!state.room || state.room.closed) {
    renderInvites([]);
    return;
  }
  try {
    renderInvites(await api('GET', `/rooms/${state.room.id}/invites`));
  } catch {
    /* ignore: the room may have just closed */
  }
}

function renderInvites(invites) {
  const room = state.room;
  $('#invites').replaceChildren(
    ...invites.map((inv) => {
      const status = inv.revoked ? 'Revocada' : inv.expired ? 'Caducada' : inv.active ? 'Activa' : 'Agotada';
      return el('tr', { class: inv.active ? '' : 'muted' }, [
        el('td', { class: 'small', text: inv.id.slice(0, 8) }),
        el('td', { class: 'small', text: new Date(inv.expiresAt).toLocaleString() }),
        el('td', { class: 'small', text: inv.maxUses ? `${inv.uses} / ${inv.maxUses}` : `${inv.uses}` }),
        el('td', { class: 'small', text: status }),
        el('td', {}, [
          inv.active
            ? el('button', {
                type: 'button', class: 'ghost small', text: 'Revocar',
                onclick: async () => {
                  if (!confirm('¿Revocar esta invitación? Quien ya esté dentro se queda; nadie más podrá entrar con ese enlace.')) return;
                  try {
                    await api('DELETE', `/rooms/${room.id}/invites/${inv.id}`);
                    await refreshInvites();
                  } catch (error) {
                    showError($('#room-error'), error);
                  }
                },
              })
            : '',
        ]),
      ]);
    }),
  );
  $('#no-invites').hidden = invites.length > 0;
}

async function refreshLayout() {
  if (!state.room || state.room.closed) {
    $('#layout-preview').replaceChildren();
    return;
  }
  try {
    renderLayout(await api('GET', `/rooms/${state.room.id}/layout`));
  } catch {
    /* ignore: the room may have just closed */
  }
}

function renderLayout(plan) {
  const svg = $('#layout-preview');
  const ns = 'http://www.w3.org/2000/svg';
  svg.setAttribute('viewBox', `0 0 ${plan.output.width} ${plan.output.height}`);
  const nodes = [];
  if (plan.tiles.length === 0) {
    const text = document.createElementNS(ns, 'text');
    text.setAttribute('x', String(plan.output.width / 2));
    text.setAttribute('y', String(plan.output.height / 2));
    text.setAttribute('text-anchor', 'middle');
    text.textContent = `${plan.title} — Comenzamos en breve`;
    nodes.push(text);
  }
  for (const tile of plan.tiles) {
    const rect = document.createElementNS(ns, 'rect');
    for (const [k, v] of Object.entries({ x: tile.rect.x, y: tile.rect.y, width: tile.rect.w, height: tile.rect.h, rx: 8 })) {
      rect.setAttribute(k, String(v));
    }
    rect.setAttribute('class', `${tile.kind === 'video' ? 'tile-video' : 'tile-card'}${tile.focus ? ' tile-focus' : ''}`);
    const label = document.createElementNS(ns, 'text');
    label.setAttribute('x', String(tile.rect.x + 16));
    label.setAttribute('y', String(tile.rect.y + tile.rect.h - 18));
    label.textContent = tile.kind === 'video' ? tile.label : `${tile.label} (sin video)`;
    nodes.push(rect, label);
  }
  svg.replaceChildren(...nodes);
}

async function mutate(path, method, body) {
  showError($('#room-error'), null);
  try {
    await api(method, path, body);
    await refreshRooms();
  } catch (error) {
    showError($('#room-error'), error);
    await refreshRooms().catch(() => {});
  }
}

// ---------------------------------------------------------------------------
// Wiring
// ---------------------------------------------------------------------------

function formValue(form, name) {
  const value = form.elements[name]?.value.trim();
  return value === '' ? undefined : value;
}

$('#login-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  showError($('#login-error'), null);
  try {
    await login($('#login-token').value.trim());
  } catch (error) {
    showError($('#login-error'), error.status === 401 ? new Error('Token no válido.') : error);
  }
});

$('#logout').addEventListener('click', logout);

$('#room-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const errorBox = form.querySelector('.error');
  showError(errorBox, null);
  try {
    const room = await api('POST', '/rooms', {
      id: formValue(form, 'id'),
      title: formValue(form, 'title'),
      host: { name: formValue(form, 'host'), avatar: formValue(form, 'avatar') },
      capacity: Number(form.capacity.value) || undefined,
      layout: form.layout.value,
    });
    form.reset();
    $('#new-room').open = false;
    await refreshRooms();
    selectRoom(room.id);
  } catch (error) {
    showError(errorBox, error);
  }
});

$('#air-start').addEventListener('click', () => mutate(`/rooms/${state.selected}/broadcast/start`, 'POST'));
$('#air-stop').addEventListener('click', () => mutate(`/rooms/${state.selected}/broadcast/stop`, 'POST'));

$('#invite-create').addEventListener('click', async () => {
  showError($('#room-error'), null);
  try {
    const invite = await api('POST', `/rooms/${state.selected}/invites`, { ttlSeconds: Number($('#invite-ttl').value), maxUses: Number($('#invite-max').value) || 0 });
    $('#invite-url').value = invite.url;
    $('#invite-expiry').textContent = new Date(invite.expiresAt).toLocaleString();
    $('#invite-result').hidden = false;
    $('#invite-url').select();
    await refreshInvites();
  } catch (error) {
    showError($('#room-error'), error);
  }
});

$('#invite-copy').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText($('#invite-url').value);
    $('#invite-copy').textContent = 'Copiado';
  } catch {
    $('#invite-url').select();
    $('#invite-copy').textContent = 'Usa Ctrl+C';
  }
  setTimeout(() => {
    $('#invite-copy').textContent = 'Copiar';
  }, 1500);
});

$('#admission-mode').addEventListener('change', (event) => mutate(`/rooms/${state.selected}`, 'PATCH', { admission: event.target.value }));
$('#layout-mode').addEventListener('change', (event) => mutate(`/rooms/${state.selected}`, 'PATCH', { layout: event.target.value }));

$('#destination-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const errorBox = form.querySelector('.error');
  showError(errorBox, null);
  try {
    await api('POST', `/rooms/${state.selected}/destinations`, {
      name: formValue(form, 'name'),
      url: formValue(form, 'url'),
      streamKey: form.streamKey.value.trim(),
      kind: form.kind.value || undefined,
    });
    form.reset();
    await refreshRooms();
  } catch (error) {
    showError(errorBox, error);
  }
});

$('#settings-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const errorBox = form.querySelector('.error');
  showError(errorBox, null);
  try {
    await api('PATCH', `/rooms/${state.selected}`, {
      title: formValue(form, 'title'),
      host: { name: formValue(form, 'host') },
      capacity: Number(form.capacity.value),
    });
    await refreshRooms();
  } catch (error) {
    showError(errorBox, error);
  }
});

$('#room-close').addEventListener('click', () => {
  if (confirm('¿Cerrar la sala? Se expulsa a todos y los enlaces dejan de funcionar.')) {
    void mutate(`/rooms/${state.selected}`, 'DELETE');
  }
});

const saved = storedToken();
if (saved) {
  login(saved).catch(() => logout());
} else {
  logout();
}
