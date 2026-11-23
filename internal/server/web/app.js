// OpenGit web UI. No framework; all dynamic text goes through textContent (no innerHTML).
(function () {
  var app = document.getElementById('app'), nav = document.getElementById('nav');
  var token = null, me = null, stopLogo = null;
  var reduce = window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches;
  try { token = sessionStorage.getItem('og_token'); } catch (e) {}
  var SVGNS = 'http://www.w3.org/2000/svg';

  function h(tag, attrs) {
    var el = document.createElement(tag);
    for (var k in (attrs || {})) {
      if (k === 'text') el.textContent = attrs[k];
      else if (k === 'on') for (var ev in attrs.on) el.addEventListener(ev, attrs.on[ev]);
      else el.setAttribute(k, attrs[k]);
    }
    for (var i = 2; i < arguments.length; i++) {
      var c = arguments[i]; if (c == null) continue;
      el.append(c.nodeType ? c : document.createTextNode(c));
    }
    return el;
  }
  function svg(tag, attrs) {
    var el = document.createElementNS(SVGNS, tag);
    for (var k in attrs) el.setAttribute(k, attrs[k]);
    return el;
  }
  function api(method, path, body) {
    var o = { method: method, headers: {} };
    if (token) o.headers.Authorization = 'Bearer ' + token;
    if (body) { o.headers['Content-Type'] = 'application/json'; o.body = JSON.stringify(body); }
    return fetch(path, o).then(function (r) {
      return (r.status === 204 ? Promise.resolve(null) : r.json().catch(function () { return {}; })).then(function (j) {
        if (!r.ok) { var e = new Error((j && j.error) || ('HTTP ' + r.status)); e.status = r.status; throw e; }
        return j;
      });
    });
  }
  function setToken(t) { token = t; try { t ? sessionStorage.setItem('og_token', t) : sessionStorage.removeItem('og_token'); } catch (e) {} }
  function enc(p) { return p.split('/').map(encodeURIComponent).join('/'); }

  // Render a page: a bento grid of tiles, entering in a staggered sequence.
  function bento() {
    if (stopLogo) { stopLogo(); stopLogo = null; }
    app.textContent = '';
    var g = h('div', { class: 'bento' });
    for (var i = 0; i < arguments.length; i++) { arguments[i].style.setProperty('--i', i); g.append(arguments[i]); }
    app.append(g);
  }
  function tile(label, cls) {
    var t = h('section', { class: 'tile ' + (cls || ''), 'aria-label': label });
    for (var i = 2; i < arguments.length; i++) if (arguments[i]) t.append(arguments[i]);
    return t;
  }
  function titled(label, cls) {
    var t = tile(label, cls); t.prepend(h('h2', { text: label }));
    for (var i = 2; i < arguments.length; i++) if (arguments[i]) t.append(arguments[i]);
    return t;
  }
  function failTile(e) { bento(tile('Error', 's12', h('p', { class: 'err', role: 'alert', text: e.message }))); }
  function countUp(el, n) {
    if (reduce || n === 0) { el.textContent = n; return; }
    var t0 = performance.now();
    (function step(t) { var k = Math.min(1, (t - t0) / 700); el.textContent = Math.round(n * (1 - Math.pow(1 - k, 3))); if (k < 1) requestAnimationFrame(step); })(t0);
  }
  function kpi(label, n, cls, unit) {
    var v = h('span', { text: '0' });
    countUp(v, n);
    return titled(label, cls || 's4', h('div', { class: 'kpi' }, v, unit ? h('small', { text: unit }) : null));
  }

  function renderNav() {
    nav.textContent = '';
    nav.append(h('a', { href: '#/', class: 'brand' }, 'Open', h('b', { text: 'Git' })));
    if (me) {
      nav.append(h('span', { class: 'who', text: me.username }),
        h('a', { href: '#/', text: 'Sign out', on: { click: function () { setToken(null); me = null; renderNav(); } } }));
    } else nav.append(h('a', { class: 'who', href: '#/login', text: 'Sign in' }));
  }

  // ---- pages ----
  function authPage() {
    var u = h('input', { placeholder: 'username', 'aria-label': 'Username', autocomplete: 'username', required: '' });
    var p = h('input', { type: 'password', placeholder: 'password (8+ characters)', 'aria-label': 'Password', autocomplete: 'current-password', required: '' });
    var msg = h('div', { class: 'err', role: 'alert' });
    function go(path) {
      return function (e) {
        e.preventDefault();
        api('POST', path, { username: u.value, password: p.value }).then(function (r) {
          setToken(r.token); me = r.user; renderNav(); location.hash = '#/';
        }).catch(function (er) { msg.textContent = er.message; });
      };
    }
    var f = h('form', { on: { submit: go('/api/login') } }, u, p, h('button', { text: 'Sign in' }),
      h('button', { type: 'button', class: 'alt', text: 'Create account', on: { click: function (e) { if (u.reportValidity() && p.reportValidity()) go('/api/register')(e); } } }));
    bento(titled('Sign in or create an account', 's6', f, msg, h('p', { class: 'sub', text: 'The first account created on a new server becomes the administrator.' })));
  }

  function home() {
    api('GET', '/api/repos').then(function (rs) {
      var priv = rs.filter(function (r) { return r.private; }).length;
      var canvas = h('canvas', { id: 'logo', 'aria-label': 'OpenGit logo: an eye zooms into its pupil, revealing the name' });
      var logo = tile('Logo', 's8 r2 flush anchor', canvas);
      var tiles = [logo, kpi('Repositories', rs.length), kpi(me ? 'Private' : 'Public', me ? priv : rs.length - priv)];
      if (me) {
        var n = h('input', { placeholder: 'new-repo-name', 'aria-label': 'Repository name', pattern: '[A-Za-z0-9][A-Za-z0-9._-]*', required: '' });
        var pr = h('input', { type: 'checkbox' });
        var msg = h('div', { class: 'err', role: 'alert' });
        tiles.push(titled('New repository', 's12', h('form', { on: { submit: function (e) {
          e.preventDefault();
          api('POST', '/api/repos', { name: n.value, private: pr.checked }).then(function (r) {
            location.hash = '#/' + r.owner + '/' + r.name;
          }).catch(function (er) { msg.textContent = er.message; });
        } } }, n, h('label', null, pr, 'private'), h('button', { text: 'Create' })), msg));
      } else {
        tiles.push(titled('Get started', 's12', h('p', { class: 'sub', text: 'Sign in to create repositories and push code.' }), h('a', { class: 'btn', href: '#/login', text: 'Sign in / sign up' })));
      }
      var list = h('ul', { class: 'rows' });
      if (!rs.length) list.append(h('li', { class: 'empty', text: 'No repositories yet.' }));
      rs.forEach(function (r) {
        list.append(h('li', null, h('span', { class: 'ico dir', text: '▣' }),
          h('a', { class: 'grow', href: '#/' + r.owner + '/' + r.name, text: r.owner + ' / ' + r.name }),
          r.description ? h('span', { class: 'meta', text: r.description.slice(0, 40) }) : null,
          r.private ? h('span', { class: 'chip priv', text: 'private' }) : h('span', { class: 'chip', text: 'public' })));
      });
      tiles.push(titled('Repositories', 's12', list));
      bento.apply(null, tiles);
      if (window.OpenGitLogo) stopLogo = OpenGitLogo.mount(canvas);
    }).catch(failTile);
  }

  function header(owner, name, meta, active) {
    var base = '#/' + owner + '/' + name;
    return [h('h1', { class: 'title' }, decodeURIComponent(owner), h('span', { text: ' / ' }), decodeURIComponent(name)),
      h('p', { class: 'sub', text: meta.repo.description || '' }),
      h('div', { class: 'tabs' }, h('a', { href: base, class: active === 'code' ? 'on' : '', text: 'Code' }),
        h('a', { href: base + '/commits', class: active === 'commits' ? 'on' : '', text: 'Commits' }))];
  }
  function withHeader(owner, name, meta, active, tiles) {
    bento.apply(null, tiles);
    var hd = header(owner, name, meta, active), first = app.firstChild;
    hd.forEach(function (e) { app.insertBefore(e, first); });
  }
  function crumbs(owner, name, path) {
    var c = h('div', { class: 'crumbs' }, h('a', { href: '#/' + owner + '/' + name, text: 'root' }));
    var acc = '';
    (path ? path.split('/') : []).forEach(function (seg, i, arr) {
      acc += (acc ? '/' : '') + seg;
      c.append(' / ', i < arr.length - 1 ? h('a', { href: '#/' + owner + '/' + name + '/tree/' + enc(acc), text: seg }) : h('span', { text: seg }));
    });
    return c;
  }
  function cloneTile(owner, name) {
    var url = location.origin + '/git/' + decodeURIComponent(owner) + '/' + decodeURIComponent(name) + '.git';
    var btn = h('button', { class: 'alt sm', text: 'Copy', on: { click: function () {
      if (navigator.clipboard) navigator.clipboard.writeText(url).then(function () { btn.textContent = 'Copied'; setTimeout(function () { btn.textContent = 'Copy'; }, 1500); });
    } } });
    return titled('Clone', 's4', h('div', { class: 'clone' }, h('code', { text: url }), btn));
  }

  function repoView(owner, name, kind, path) {
    api('GET', '/api/repos/' + owner + '/' + name).then(function (meta) {
      var about = titled('About', 's4', h('p', { class: 'sub', text: meta.repo.description || 'No description.' }),
        h('span', { class: 'chip' + (meta.repo.private ? ' priv' : ''), text: meta.repo.private ? 'private' : 'public' }),
        meta.default_branch ? h('span', { class: 'meta', text: '  branch ' + meta.default_branch }) : null);
      if (meta.empty) {
        var url = location.origin + '/git/' + decodeURIComponent(owner) + '/' + decodeURIComponent(name) + '.git';
        return withHeader(owner, name, meta, 'code', [
          titled('Empty repository', 's8', h('p', { class: 'sub', text: 'Push your first commit:' }), h('pre', { text: 'git remote add origin ' + url + '\ngit push -u origin main' })),
          about]);
      }
      var q = '?path=' + encodeURIComponent(path || ''), base = '/api/repos/' + owner + '/' + name;
      if (kind === 'commits') return commitsView(owner, name, meta, base);
      if (kind === 'blob') {
        return api('GET', base + '/blob' + q).then(function (b) {
          withHeader(owner, name, meta, 'code', [titled('File', 's12', crumbs(owner, name, path),
            b.binary ? h('p', { class: 'empty', text: 'Binary file (' + b.size + ' bytes).' }) : h('pre', { class: 'file', text: b.content }))]);
        }).catch(function (e) { withHeader(owner, name, meta, 'code', [titled('File', 's12', crumbs(owner, name, path), h('p', { class: 'err', text: e.message }))]); });
      }
      return api('GET', base + '/tree' + q).then(function (es) {
        var ul = h('ul', { class: 'rows' });
        if (path) { var up = path.split('/').slice(0, -1).join('/'); ul.append(h('li', null, h('span', { class: 'ico', text: '↰' }), h('a', { class: 'grow', href: '#/' + owner + '/' + name + (up ? '/tree/' + enc(up) : ''), text: '..' }))); }
        es.forEach(function (e) {
          var full = (path ? path + '/' : '') + e.name, dir = e.type === 'tree';
          ul.append(h('li', null, h('span', { class: 'ico' + (dir ? ' dir' : ''), text: dir ? '▸' : '·' }),
            h('a', { class: 'grow', href: '#/' + owner + '/' + name + '/' + (dir ? 'tree' : 'blob') + '/' + enc(full), text: e.name }),
            dir ? null : h('span', { class: 'meta', text: e.size + ' B' })));
        });
        var tiles = [titled('Files', 's8 r2', crumbs(owner, name, path), ul), about, cloneTile(owner, name)];
        withHeader(owner, name, meta, 'code', tiles);
        var readme = es.filter(function (e) { return /^readme(\.md|\.txt)?$/i.test(e.name) && e.type === 'blob'; })[0];
        if (readme) api('GET', base + '/blob?path=' + encodeURIComponent((path ? path + '/' : '') + readme.name)).then(function (b) {
          if (b.binary) return;
          var t = titled(readme.name, 's12', h('pre', { class: 'file', text: b.content }));
          t.style.setProperty('--i', 4); app.querySelector('.bento').append(t);
        }).catch(function () {});
      });
    }).catch(failTile);
  }

  // Commits: each commit is a dot on a timeline; drag across the chart to filter the list (Esc clears).
  function commitsView(owner, name, meta, base) {
    return api('GET', base + '/commits?limit=100').then(function (cs) {
      var list = h('ul', { class: 'rows' }), summary = h('span', { class: 'grow', 'aria-live': 'polite' });
      var from = h('input', { type: 'date', 'aria-label': 'From date' }), to = h('input', { type: 'date', 'aria-label': 'To date' });
      var times = cs.map(function (c) { return Date.parse(c.date); });
      var tmin = Math.min.apply(null, times), tmax = Math.max.apply(null, times);
      if (tmax - tmin < 864e5) { tmin -= 432e5; tmax += 432e5; }
      var authors = []; cs.forEach(function (c) { if (authors.indexOf(c.author) < 0) authors.push(c.author); });
      var lanes = Math.min(authors.length, 6), W = 760, Hh = 40 + lanes * 28, padL = 12, padR = 12;
      function xOf(t) { return padL + (t - tmin) / (tmax - tmin) * (W - padL - padR); }
      var s = svg('svg', { class: 'chart', viewBox: '0 0 ' + W + ' ' + Hh, role: 'img', 'aria-label': 'Commit timeline. Drag to select a range; use the date fields as an alternative.' });
      s.append(svg('line', { class: 'axis', x1: padL, x2: W - padR, y1: Hh - 22, y2: Hh - 22 }));
      [tmin, (tmin + tmax) / 2, tmax].forEach(function (t, i) {
        var tx = svg('text', { x: xOf(t), y: Hh - 6, 'text-anchor': i === 0 ? 'start' : i === 2 ? 'end' : 'middle' });
        tx.textContent = new Date(t).toISOString().slice(0, 10); s.append(tx);
      });
      var dots = cs.map(function (c, i) {
        var lane = Math.min(authors.indexOf(c.author), lanes - 1);
        var d = svg('circle', { cx: xOf(times[i]), cy: 18 + lane * 28, r: 5 });
        var t = svg('title', {}); t.textContent = c.subject + ' (' + c.author + ')'; d.append(t); s.append(d); return d;
      });
      var brush = svg('rect', { class: 'brush', y: 2, height: Hh - 24, width: 0, visibility: 'hidden' }); s.append(brush);
      var rows = cs.map(function (c) {
        var li = h('li', null, h('span', { class: 'grow', text: c.subject }), h('span', { class: 'meta', text: c.hash.slice(0, 7) + ' · ' + c.author + ' · ' + c.date.slice(0, 10) }));
        list.append(li); return li;
      });
      function apply(lo, hi) { // lo/hi in ms, or null to clear
        var n = 0;
        cs.forEach(function (c, i) {
          var inside = lo === null || (times[i] >= lo && times[i] <= hi);
          rows[i].hidden = !inside; dots[i].classList.toggle('out', !inside); if (inside) n++;
        });
        summary.textContent = lo === null ? cs.length + ' commits' : n + ' of ' + cs.length + ' commits · ' + new Date(lo).toISOString().slice(0, 10) + ' → ' + new Date(hi).toISOString().slice(0, 10);
        if (lo === null) { brush.setAttribute('visibility', 'hidden'); from.value = to.value = ''; }
        else { from.value = new Date(lo).toISOString().slice(0, 10); to.value = new Date(hi).toISOString().slice(0, 10); }
      }
      function showBrush(lo, hi) { brush.setAttribute('visibility', 'visible'); brush.setAttribute('x', xOf(lo)); brush.setAttribute('width', Math.max(2, xOf(hi) - xOf(lo))); }
      function tAt(ev) { var r = s.getBoundingClientRect(); return tmin + clamp((ev.clientX - r.left) / r.width * W - padL, 0, W - padL - padR) / (W - padL - padR) * (tmax - tmin); }
      function clamp(v, a, b) { return v < a ? a : v > b ? b : v; }
      var anchor = null;
      s.addEventListener('pointerdown', function (e) { anchor = tAt(e); s.setPointerCapture(e.pointerId); });
      s.addEventListener('pointermove', function (e) { if (anchor === null) return; var t = tAt(e), lo = Math.min(anchor, t), hi = Math.max(anchor, t); showBrush(lo, hi); apply(lo, hi); });
      s.addEventListener('pointerup', function (e) { if (anchor !== null && Math.abs(tAt(e) - anchor) < (tmax - tmin) * 0.01) apply(null); anchor = null; });
      function fromInputs() {
        if (!from.value && !to.value) return apply(null);
        var lo = from.value ? Date.parse(from.value) : tmin, hi = to.value ? Date.parse(to.value) + 864e5 - 1 : tmax;
        showBrush(clamp(lo, tmin, tmax), clamp(hi, tmin, tmax)); apply(lo, hi);
      }
      from.addEventListener('change', fromInputs); to.addEventListener('change', fromInputs);
      s.tabIndex = 0;
      s.addEventListener('keydown', function (e) { if (e.key === 'Escape') apply(null); });
      var clear = h('button', { class: 'alt sm', text: 'Clear', on: { click: function () { apply(null); } } });
      apply(null);
      withHeader(owner, name, meta, 'commits', [
        titled('Activity: drag to filter', 's12', s, h('div', { class: 'chartbar' }, summary, h('label', null, 'From', from), h('label', null, 'To', to), clear)),
        titled('Commits', 's12', list)]);
    }).catch(failTile);
  }

  function route() {
    var parts = (location.hash.replace(/^#\/?/, '') || '').split('/').filter(Boolean).map(decodeURIComponent);
    if (!parts.length) return home();
    if (parts[0] === 'login') return authPage();
    if (parts.length < 2) return home();
    var kind = parts[2] || 'tree', path = parts.slice(3).join('/');
    if (kind !== 'tree' && kind !== 'blob' && kind !== 'commits') kind = 'tree';
    repoView(encodeURIComponent(parts[0]), encodeURIComponent(parts[1]), kind, path);
  }

  window.addEventListener('hashchange', route);
  (token ? api('GET', '/api/user').then(function (u) { me = u; }).catch(function () { setToken(null); }) : Promise.resolve())
    .then(function () { renderNav(); route(); });
})();
