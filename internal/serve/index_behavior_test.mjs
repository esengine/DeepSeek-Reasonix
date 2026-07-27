import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const html = readFileSync(new URL('./index.html', import.meta.url), 'utf8');

function between(source, start, end) {
  const from = source.indexOf(start);
  assert.notEqual(from, -1, `missing production marker: ${start}`);
  const to = source.indexOf(end, from);
  assert.notEqual(to, -1, `missing production marker: ${end}`);
  return source.slice(from, to);
}

class ClassList {
  constructor(node) { this.node = node; }
  toggle(name, force) {
    const names = new Set(this.node.className.split(/\s+/).filter(Boolean));
    const on = force === undefined ? !names.has(name) : force;
    if (on) names.add(name); else names.delete(name);
    this.node.className = [...names].join(' ');
    return on;
  }
}

class TestNode {
  constructor(tag = 'div', ownerDocument = null) {
    this.tagName = tag.toUpperCase();
    this.ownerDocument = ownerDocument;
    this.childNodes = [];
    this.parentNode = null;
    this.className = '';
    this.dataset = {};
    this.attributes = {};
    this.disabled = false;
    this.removed = false;
    this._text = '';
    this.classList = new ClassList(this);
  }
  set textContent(value) { this._text = String(value ?? ''); this.childNodes = []; }
  get textContent() { return this._text + this.childNodes.map(child => child.textContent).join(''); }
  set innerHTML(value) {
    this._innerHTML = String(value);
    this.childNodes = [];
    for (const name of ['data-cap-risk-findings', 'data-cap-warnings']) {
      if (this._innerHTML.includes(name)) {
        const node = new TestNode('div', this.ownerDocument);
        node.attributes[name] = '';
        this.appendChild(node);
      }
    }
    const buttons = /<button\s+([^>]*)>/g;
    for (let match; (match = buttons.exec(this._innerHTML));) {
      const node = new TestNode('button', this.ownerDocument);
      const attrs = match[1];
      const cls = /class="([^"]*)"/.exec(attrs);
      if (cls) node.className = cls[1];
      for (const attr of attrs.matchAll(/data-([a-z-]+)="([^"]*)"/g)) {
        const key = attr[1].replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
        node.dataset[key] = attr[2];
      }
      this.appendChild(node);
    }
  }
  get innerHTML() { return this._innerHTML || ''; }
  appendChild(child) { child.parentNode = this; this.childNodes.push(child); return child; }
  remove() {
    this.removed = true;
    if (this.parentNode) this.parentNode.childNodes = this.parentNode.childNodes.filter(child => child !== this);
    this.parentNode = null;
  }
  contains(node) {
    return node === this || this.childNodes.some(child => child.contains(node));
  }
  focus() { if (this.ownerDocument) this.ownerDocument.activeElement = this; }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  querySelectorAll(selector) {
    const matches = node => {
      if (selector.startsWith('.')) return node.className.split(/\s+/).includes(selector.slice(1));
      if (selector.startsWith('[')) return Object.hasOwn(node.attributes, selector.slice(1, -1));
      return node.tagName.toLowerCase() === selector.toLowerCase();
    };
    const found = [];
    const visit = node => {
      for (const child of node.childNodes) {
        if (matches(child)) found.push(child);
        visit(child);
      }
    };
    visit(this);
    return found;
  }
}

class TestDocument {
  constructor() {
    this.listeners = new Map();
    this.activeElement = null;
  }
  createElement(tag) { return new TestNode(tag, this); }
  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }
  removeEventListener(type, listener) {
    this.listeners.set(type, (this.listeners.get(type) || []).filter(item => item !== listener));
  }
  dispatchKey(key) {
    const event = {
      key,
      target: this.activeElement,
      ctrlKey: false,
      metaKey: false,
      altKey: false,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    };
    for (const listener of [...(this.listeners.get('keydown') || [])]) listener(event);
    return event;
  }
}

function makeHarness() {
  const document = new TestDocument();
  const context = vm.createContext({ console, setTimeout, clearTimeout });
  const prelude = `
    const document=globalThis.__document;
    const log=document.createElement('div');
    const input=document.createElement('textarea'); input.value='';
    const btnSend=document.createElement('button');
    document.activeElement=input;
    let yoloAckBanner=null,yoloAckPending=false;
    const notices=[];
    let postImpl=()=>Promise.resolve({ok:true,status:204,text:async()=>'',json:async()=>({})});
    const post=(url,body)=>postImpl(url,body);
    const __=key=>key;
    const escHtml=value=>String(value??'').replace(/[&<>"']/g,ch=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
    const el=(tag,cls='',text='')=>{const node=document.createElement(tag);node.className=cls;if(text!==undefined&&text!=='')node.textContent=text;return node;};
    const isTextEntry=()=>false;
    const isPlainKey=e=>!e.ctrlKey&&!e.metaKey&&!e.altKey;
    const scrollDown=()=>{};
    const showNotice=(message,level)=>notices.push({message,level});
    const setConnState=()=>{};
    const clearRetrying=()=>{};
    const es={};
    globalThis.__api={
      document,log,input,btnSend,notices,es,
      setPostImpl:fn=>{postImpl=fn;},
      capability:approval=>showCapabilityApproval(approval),
      yolo:state=>renderYOLOPolicyState(state),
    };
  `;
  context.__document = document;
  vm.runInContext(prelude, context);
  vm.runInContext(between(html, 'let pendingPrompts=[];', '// ── ask ──'), context);
  vm.runInContext(between(html, 'es.onmessage=ev=>', '\nes.onerror='), context);
  return context.__api;
}

function capabilityApproval() {
  return {
    id: 'cap-1',
    sandbox_capability: {
      argv: ['printf', 'a b'],
      grant_prefix: ['printf'],
      canonical_executable: '/usr/bin/printf',
      reusable: true,
      suspected_secret: false,
      warnings: ['warning <img src=x onerror=boom()>'],
      review: {
        risk: {
          level: 'high',
          findings: [{ code: 'RISK', message: 'finding <script>boom()</script>' }],
        },
        effective_delta: {},
      },
    },
  };
}

const flush = () => new Promise(resolve => setImmediate(resolve));

test('capability letter and numeric shortcuts execute the production action map', async () => {
  const expected = ['allow_once', 'allow_session', 'allow_persistent', 'run_sandboxed', 'cancel_command'];
  for (const [index, key] of [...['Y', 'A', 'P', 'S', 'C'], ...['1', '2', '3', '4', '5']].entries()) {
    const api = makeHarness();
    const calls = [];
    api.setPostImpl(async (url, body) => {
      calls.push({ url, body });
      return { ok: true, status: 204, text: async () => '' };
    });
    api.capability(capabilityApproval());
    const event = api.document.dispatchKey(key);
    await flush();
    assert.equal(event.defaultPrevented, true, `shortcut ${key} was not consumed`);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/capability-approve');
    assert.equal(calls[0].body.id, 'cap-1');
    assert.equal(calls[0].body.action, expected[index % expected.length]);
  }
});

test('risk findings and warnings render as separate text-only DOM sections', () => {
  const api = makeHarness();
  api.capability(capabilityApproval());
  const card = api.log.querySelector('.cap-approval');
  const riskRoot = card.querySelector('[data-cap-risk-findings]');
  const warningRoot = card.querySelector('[data-cap-warnings]');
  assert.ok(riskRoot.querySelector('.cap-approval__details'));
  assert.ok(warningRoot.querySelector('.cap-approval__details--warning'));
  assert.match(riskRoot.textContent, /RISK: finding <script>boom\(\)<\/script>/);
  assert.match(warningRoot.textContent, /warning <img src=x onerror=boom\(\)>/);
  assert.equal(riskRoot.querySelector('script'), null);
  assert.equal(warningRoot.querySelector('img'), null);
});

for (const failure of ['reject', 'non-2xx']) {
  test(`capability ${failure} keeps the card enabled and retryable`, async () => {
    const api = makeHarness();
    let attempts = 0;
    api.setPostImpl(async () => {
      attempts++;
      if (attempts === 1) {
        if (failure === 'reject') throw new Error('offline');
        return { ok: false, status: 409, text: async () => 'stale' };
      }
      return { ok: true, status: 204, text: async () => '' };
    });
    api.capability(capabilityApproval());
    const card = api.log.querySelector('.cap-approval');
    api.document.dispatchKey('Y');
    await flush();
    assert.equal(card.removed, false);
    assert.ok(card.querySelectorAll('.cap-approval__btn').every(button => !button.disabled));
    api.document.dispatchKey('Y');
    await flush();
    assert.equal(attempts, 2);
    assert.equal(card.removed, true);
  });
}

test('headless warning notice executes the production notice branch without a YOLO banner', () => {
  const api = makeHarness();
  api.es.onmessage({ data: JSON.stringify({
    kind: 'notice',
    level: 'warn',
    code: 'sandbox_capability_project_expansion',
    text: 'headless warning',
  }) });
  assert.ok(api.log.querySelector('.notice--warn'));
  assert.equal(api.log.querySelector('.yolo-ack'), null);
  assert.equal(api.input.disabled, false);
});

for (const failure of ['reject', 'non-2xx']) {
  test(`YOLO acknowledgement ${failure} keeps the banner blocking and retryable`, async () => {
    const api = makeHarness();
    let attempts = 0;
    api.setPostImpl(async () => {
      attempts++;
      if (attempts === 1) {
        if (failure === 'reject') throw new Error('offline');
        return { ok: false, status: 500, json: async () => { throw new Error('no json'); } };
      }
      return {
        ok: true,
        status: 200,
        json: async () => ({ state: { acknowledgement: 'accepted', yolo: true, interactive: true, effective: true } }),
      };
    });
    api.yolo({ acknowledgement: 'required', yolo: true, interactive: true, effective: true });
    const banner = api.log.querySelector('.yolo-ack');
    api.document.dispatchKey('Y');
    await flush();
    assert.equal(banner.removed, false);
    assert.equal(api.input.disabled, true);
    assert.ok(banner.querySelectorAll('.yolo-ack__btn').every(button => !button.disabled));
    api.document.dispatchKey('Y');
    await flush();
    assert.equal(attempts, 2);
    assert.equal(banner.removed, true);
    assert.equal(api.input.disabled, false);
  });
}
