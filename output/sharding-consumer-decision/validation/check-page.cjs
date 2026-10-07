// Lightweight, non-browser DOM harness for the standalone page's control logic.
// This checks event behavior; it does not render or validate visual layout.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
let nodes = [];
class Element {
  constructor(tag, attrs) {
    this.tag = tag; this.attrs = {...attrs}; this.listeners = {};
    this.dataset = Object.fromEntries(Object.entries(attrs).filter(([k]) => k.startsWith('data-')).map(([k,v]) => [k.slice(5),v]));
    this.value = attrs.id === 'source-filter' ? 'all' : '';
    this.classList = {toggle() {}}; this.open = false;
    this._html = ''; this.textContent = ''; this.hidden = 'hidden' in attrs;
  }
  getAttribute(k) { return this.attrs[k]; }
  setAttribute(k,v) { this.attrs[k] = v; }
  get id() { return this.attrs.id; }
  addEventListener(name, fn) { (this.listeners[name] ??= []).push(fn); }
  dispatch(name) { (this.listeners[name] || []).forEach(fn => fn({currentTarget:this})); }
  focus() { document.activeElement = this; }
  scrollIntoView() {}
  set innerHTML(s) {
    this._html = s;
    if (this.id === 'source-grid') {
      nodes = nodes.filter(x => !x.dataset.source);
      for (const match of s.matchAll(/<button\s+([^>]+)>/g)) {
        const attrs = Object.fromEntries([...match[1].matchAll(/([\w-]+)="([^"]*)"/g)].map(x=>[x[1],x[2]]));
        nodes.push(new Element('button',attrs));
      }
    }
  }
  get innerHTML() { return this._html; }
}
nodes = JSON.parse(fs.readFileSync(process.argv[2],'utf8')).map(n => new Element(n.tag,n.attrs));
function matches(el,s) {
  if (s.startsWith('.')) return (el.attrs.class || '').split(' ').includes(s.slice(1));
  const m = s.match(/^\[([\w-]+)(?:=([^\]]+))?\]$/);
  return m ? (m[2] === undefined ? m[1] in el.attrs : el.attrs[m[1]] === m[2]) : el.tag === s;
}
const document = {
  getElementById(id) { const n=nodes.find(n=>n.id===id); assert(n, 'missing ID '+id); return n; },
  querySelectorAll(s) { return nodes.filter(n=>matches(n,s)); },
  querySelector(s) { return nodes.find(n=>matches(n,s)); },
  activeElement: null,
};
const window = {innerWidth:1200, listeners:{}, addEventListener(k,fn){this.listeners[k]=fn;}, print(){this.listeners.beforeprint();this.listeners.afterprint();}};
const history = {replaceState(){}};
vm.runInNewContext(fs.readFileSync(process.argv[3],'utf8'), {document,window,history,location:{hash:''}});
const id = x=>document.getElementById(x);
for (let n=1;n<=6;n++) {
  id('tab-'+n).dispatch('click');
  const visible = document.querySelectorAll('[role=tabpanel]').filter(x=>!x.hidden);
  assert.equal(visible.length,1); assert.equal(visible[0].id,'task-'+n);
  assert.equal(id('tab-'+n).getAttribute('aria-selected'),'true');
}
id('congestion-toggle').dispatch('click');
assert(id('flow-result').textContent.includes('Backpressure'));
id('congestion-toggle').dispatch('click');
assert(id('flow-result').textContent.includes('advance'));
for (const b of document.querySelectorAll('[data-code]')) {
  b.dispatch('click'); assert(id('contract-code').innerHTML.length > 200);
}
assert.equal(document.querySelectorAll('[data-source]').length,16);
for (const b of [...document.querySelectorAll('[data-source]')]) {
  b.dispatch('click'); assert(id('source-detail').innerHTML.includes('loki.source.'+b.dataset.source));
}
for (const [category,count] of [['direct',3],['coordinate',10],['adapt',3],['all',16]]) {
  id('source-filter').value=category; id('source-filter').dispatch('change');
  assert.equal(document.querySelectorAll('[data-source]').length,count);
}
for (const b of document.querySelectorAll('[data-scenario]')) {
  b.dispatch('click'); assert.equal(b.getAttribute('aria-pressed'),'true');
  assert.equal((id('lanes-grid').innerHTML.match(/class="lane-panel"/g)||[]).length,3);
}
id('print-button').dispatch('click');
console.log('PASS: six task tabs, backpressure toggle, both pseudocode views, 16 source details, four filters, three scheduling scenarios, print callbacks.');
console.log('Scope: JavaScript interaction logic in a lightweight DOM harness; no browser rendering.');
