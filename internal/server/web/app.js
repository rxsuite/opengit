// OpenGit web UI. No framework; all dynamic text goes through textContent (no innerHTML).
(function () {
  var app = document.getElementById('app'), nav = document.getElementById('nav');
  var token = null, me = null;
  try { token = sessionStorage.getItem('og_token'); } catch (e) {}

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
  function show() { app.textContent = ''; for (var i = 0; i < arguments.length; i++) app.append(arguments[i]); }
  function fail(e) { show(h('p', { class: 'err', text: e.message })); }

  function renderNav() {
    nav.textContent = '';
    nav.append(h('a', { href: '#/', text: 'Home' }));
    if (me) {
      nav.append(h('span', { text: me.username }), h('a', { href: '#/', text: 'Sign out', on: { click: function () { setToken(null); me = null; } } }));
    } else nav.append(h('a', { href: '#/login', text: 'Sign in' }));
  }

  function authForm() {
    var u = h('input', { placeholder: 'username', 'aria-label': 'Username', autocomplete: 'username', required: '' });
    var p = h('input', { type: 'password', placeholder: 'password (8+ chars)', 'aria-label': 'Password', autocomplete: 'current-password', required: '' });
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
    return h('div', null, h('h2', { text: 'Sign in' }), f, msg);
  }

  function home() {
    var wrap = h('div'), canvas = h('canvas', { id: 'logo', 'aria-label': 'OpenGit: an eye zooms into its pupil, revealing the name' });
    wrap.append(canvas);
    if (me) {
      var n = h('input', { placeholder: 'new-repo-name', 'aria-label': 'Repository name', pattern: '[A-Za-z0-9][A-Za-z0-9._-]*', required: '' });
      var pr = h('input', { type: 'checkbox', id: 'priv' });
      var msg = h('div', { class: 'err', role: 'alert' });
      wrap.append(h('h2', { text: 'New repository' }), h('form', { on: { submit: function (e) {
        e.preventDefault();
        api('POST', '/api/repos', { name: n.value, private: pr.checked }).then(function (r) {
          location.hash = '#/' + r.owner + '/' + r.name;
        }).catch(function (er) { msg.textContent = er.message; });
      } } }, n, h('label', null, pr, ' private'), h('button', { text: 'Create' })), msg);
    }
    var list = h('ul', { class: 'list' });
    wrap.append(h('h2', { text: 'Repositories' }), list);
    api('GET', '/api/repos').then(function (rs) {
      if (!rs.length) list.append(h('li', { text: 'No repositories yet.' }));
      rs.forEach(function (r) {
        list.append(h('li', null, h('a', { href: '#/' + r.owner + '/' + r.name, text: r.owner + '/' + r.name }),
          r.private ? h('span', { class: 'tag', text: 'private' }) : null));
      });
    }).catch(fail);
    show(wrap);
    if (window.OpenGitLogo) OpenGitLogo.mount(canvas);
  }

  function crumbs(owner, name, path) {
    var c = h('div', { class: 'crumbs' }, h('a', { href: '#/' + owner + '/' + name, text: owner + '/' + name }));
    var acc = '';
    (path ? path.split('/') : []).forEach(function (seg, i, arr) {
      acc += (acc ? '/' : '') + seg;
      c.append(' / ', i < arr.length - 1 ? h('a', { href: '#/' + owner + '/' + name + '/tree/' + enc(acc), text: seg }) : h('span', { text: seg }));
    });
    return c;
  }
  function tabs(owner, name, cur) {
    var b = '#/' + owner + '/' + name;
    return h('div', { class: 'tabs' }, h('a', { href: b, class: cur === 'code' ? 'on' : '', text: 'Code' }),
      h('a', { href: b + '/commits', class: cur === 'commits' ? 'on' : '', text: 'Commits' }));
  }

  function repoView(owner, name, kind, path) {
    api('GET', '/api/repos/' + owner + '/' + name).then(function (meta) {
      var top = h('div', null, h('h2', { class: 'big', text: owner + '/' + name }),
        meta.repo.description ? h('p', { text: meta.repo.description }) : null, tabs(owner, name, kind === 'commits' ? 'commits' : 'code'));
      if (meta.empty) {
        var url = location.origin + '/git/' + owner + '/' + name + '.git';
        return show(top, h('p', { text: 'This repository is empty. Push your first commit:' }),
          h('pre', { text: 'git remote add origin ' + url + '\ngit push -u origin main' }));
      }
      var q = '?path=' + encodeURIComponent(path || '');
      if (kind === 'commits') {
        return api('GET', '/api/repos/' + owner + '/' + name + '/commits').then(function (cs) {
          var ul = h('ul', { class: 'list' });
          cs.forEach(function (c) { ul.append(h('li', null, h('span', { text: c.subject }), h('code', { text: c.hash.slice(0, 7) + ' · ' + c.author + ' · ' + c.date.slice(0, 10) }))); });
          show(top, ul);
        });
      }
      if (kind === 'blob') {
        return api('GET', '/api/repos/' + owner + '/' + name + '/blob' + q).then(function (b) {
          show(top, crumbs(owner, name, path), b.binary ? h('p', { text: 'Binary file (' + b.size + ' bytes).' }) : h('pre', { class: 'file', text: b.content }));
        }).catch(function (e) { show(top, crumbs(owner, name, path), h('p', { class: 'err', text: e.message })); });
      }
      return api('GET', '/api/repos/' + owner + '/' + name + '/tree' + q).then(function (es) {
        var ul = h('ul', { class: 'list' });
        if (path) { var up = path.split('/').slice(0, -1).join('/'); ul.append(h('li', null, h('a', { href: '#/' + owner + '/' + name + (up ? '/tree/' + enc(up) : ''), text: '..' }))); }
        es.forEach(function (e) {
          var full = (path ? path + '/' : '') + e.name;
          ul.append(h('li', null, h('a', { href: '#/' + owner + '/' + name + '/' + (e.type === 'tree' ? 'tree' : 'blob') + '/' + enc(full), text: (e.type === 'tree' ? '📁 ' : '📄 ') + e.name }),
            e.type === 'blob' ? h('code', { text: e.size + ' B' }) : null));
        });
        var readme = es.filter(function (e) { return /^readme(\.md|\.txt)?$/i.test(e.name) && e.type === 'blob'; })[0];
        var out = [top, crumbs(owner, name, path), ul];
        show.apply(null, out);
        if (readme) api('GET', '/api/repos/' + owner + '/' + name + '/blob?path=' + encodeURIComponent((path ? path + '/' : '') + readme.name)).then(function (b) {
          if (!b.binary) app.append(h('h2', { text: readme.name }), h('pre', { class: 'file', text: b.content }));
        }).catch(function () {});
      });
    }).catch(fail);
  }

  function route() {
    var parts = (location.hash.replace(/^#\/?/, '') || '').split('/').filter(Boolean).map(decodeURIComponent);
    if (!parts.length) return home();
    if (parts[0] === 'login') return show(authForm());
    if (parts.length < 2) return home();
    var kind = parts[2] || 'tree', path = parts.slice(3).join('/');
    if (kind !== 'tree' && kind !== 'blob' && kind !== 'commits') kind = 'tree';
    repoView(encodeURIComponent(parts[0]), encodeURIComponent(parts[1]), kind, path);
  }

  window.addEventListener('hashchange', route);
  (token ? api('GET', '/api/user').then(function (u) { me = u; }).catch(function () { setToken(null); }) : Promise.resolve())
    .then(function () { renderNav(); route(); });
})();
