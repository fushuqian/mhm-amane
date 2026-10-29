// Amane 精简版前端：可在 Mica Home 模块面板内挂载，也可作为独立页面打开。
// 只依赖宿主注入的 api.request 或同源 fetch，不使用任何构建工具。

const BASES = (() => {
  try {
    const dir = new URL('.', import.meta.url).pathname;
    if (dir.endsWith('/ui/')) {
      const moduleRoot = dir.replace(/ui\/$/, '').replace(/\/$/, '');
      return { asset: `${moduleRoot}/ui`, api: moduleRoot };
    }
  } catch {
    /* 独立页面 */
  }
  return { asset: '', api: '' };
})();
const MODULE_BASE = BASES.api;
const ASSET_BASE = BASES.asset;

let styleInjected = false;
let activeTimer = null;

function injectStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.id = 'mh-amane-style';
  link.href = `${ASSET_BASE}/style.css`;
  document.head.appendChild(link);
}

async function request(api, path, init) {
  if (api && api.request && !init) return api.request(path);
  const res = await fetch(`${MODULE_BASE}${path}`, init);
  if (!res.ok) {
    let detail = `${res.status}`;
    try {
      const body = await res.json();
      if (body && body.error) detail = body.error;
    } catch {
      /* 非 JSON 错误体 */
    }
    throw new Error(detail);
  }
  if (res.status === 204) return null;
  return res.json();
}

function imageURL(url) {
  if (!url) return '';
  return `${MODULE_BASE}/api/image?url=${encodeURIComponent(url)}`;
}

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (ch) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[ch]));
}

function el(tag, attrs = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'html') node.innerHTML = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else node.setAttribute(key, value);
  }
  for (const child of children) node.append(child);
  return node;
}

function toast(text) {
  let box = document.querySelector('.mh-amane-toast');
  if (!box) {
    box = el('div', { class: 'mh-amane-toast' });
    document.body.appendChild(box);
  }
  box.textContent = text;
  box.classList.add('show');
  clearTimeout(box._timer);
  box._timer = setTimeout(() => box.classList.remove('show'), 2400);
}

const TASK_KIND = { sync: '自动整理', scan: '扫描', scrape: '刮削', organize: '整理' };
const TASK_STATUS = { queued: '排队中', running: '进行中' };

// 模板占位符说明，顺序与分组沿用原版。
const PLACEHOLDERS = [
  ['{number}', '番号，如 MIDV-123'],
  ['{prefix}', '番号前缀，如 MIDV'],
  ['{suffix}', '番号数字段，如 123'],
  ['{title}', '标题；为空时回退番号'],
  ['{actor}', '第一位出演者'],
  ['{actors}', '全部出演者（逗号分隔）'],
  ['{actress}', '第一位女优（同 {actor}）'],
  ['{actresses}', '全部女优（逗号分隔）'],
  ['{studio}', '制作商'],
  ['{publisher}', '发行商'],
  ['{series}', '系列'],
  ['{year}', '发行年份'],
  ['{release}', '发行日期'],
  ['{content_type}', '内容类型 censored / uncensored …'],
  ['{ext}', '源文件扩展名（不含点）'],
  ['{cd?}', '分集号：1 / 2 / 空'],
  ['{sub?}', '有字幕输出 C，否则为空'],
  ['{mosaic?}', 'censored / uncensored / cracked / leaked / 空'],
  ['{def?}', '清晰度：1080p / 720p / 空'],
  ['{raw_name}', '源文件名（不含扩展名）'],
  ['{raw_dir}', '源文件所在目录名'],
  ['{video_dir}', '最终视频目录（相对片库根，后段取值）'],
  ['{video_name}', '最终视频文件名（后段取值）'],
  ['{video_relpath}', '最终视频相对路径（后段取值）'],
];

export function mount(root, api) {
  injectStyle();
  const state = { tab: 'library', query: '', detail: null };
  root.innerHTML = '';
  root.classList.add('mh-amane');

  const header = el('header', { class: 'mh-amane-head' }, [
    el('div', { class: 'mh-amane-title' }, [el('b', { text: 'Amane 影库' })]),
  ]);
  const tabs = el('div', { class: 'mh-amane-tabs' });
  for (const [key, label] of [['library', '新片速递'], ['tasks', '任务'], ['settings', '设置']]) {
    const button = el('button', { type: 'button', text: label });
    button.addEventListener('click', () => {
      state.tab = key;
      state.detail = null;
      render();
    });
    button.dataset.tab = key;
    tabs.append(button);
  }
  header.append(tabs);

  const body = el('div', { class: 'mh-amane-body' });
  root.append(header, body);

  function stopTimer() {
    if (activeTimer) {
      clearInterval(activeTimer);
      activeTimer = null;
    }
  }

  function setTabActive() {
    for (const btn of tabs.querySelectorAll('button')) {
      btn.classList.toggle('active', btn.dataset.tab === state.tab);
    }
  }

  async function render() {
    stopTimer();
    setTabActive();
    body.innerHTML = '';
    if (state.detail) return renderDetail(state.detail);
    if (state.tab === 'library') return renderLibrary();
    if (state.tab === 'tasks') return renderTasks();
    return renderSettings();
  }

  async function renderLibrary() {
    const toolbar = el('div', { class: 'mh-amane-toolbar' });
    const search = el('input', { type: 'search', placeholder: '搜索番号或标题', value: state.query });
    search.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        state.query = search.value.trim();
        render();
      }
    });
    toolbar.append(search, el('span', { class: 'mh-amane-count', text: '加载中…' }));
    const grid = el('div', { class: 'mh-amane-grid' });
    body.append(toolbar, grid);
    try {
      const data = await request(api, `/api/metadata?limit=10&q=${encodeURIComponent(state.query)}`);
      const items = data.items || [];
      const total = data.total || 0;
      toolbar.querySelector('.mh-amane-count').textContent = `最新 ${items.length} 条 / 共 ${total} 条`;
      if (!items.length) {
        grid.append(el('div', {
          class: 'mh-amane-empty',
          text: state.query ? '没有匹配的条目' : '扫描下载库后，新入库的影片会出现在这里',
        }));
        return;
      }
      for (const item of items) {
        const card = el('button', { type: 'button', class: 'mh-amane-card' }, [
          item.poster
            ? el('img', { src: imageURL(item.poster), loading: 'lazy', alt: '' })
            : el('div', { class: 'mh-amane-noposter', text: item.number }),
          el('div', { class: 'mh-amane-card-body' }, [
            el('b', { text: item.number }),
            el('span', { text: item.title || '—' }),
            el('em', { text: item.content_type }),
          ]),
        ]);
        card.addEventListener('click', async () => {
          try {
            const detail = await request(api, `/api/metadata/${encodeURIComponent(item.number)}`);
            state.detail = detail;
            render();
          } catch (err) {
            toast(err.message);
          }
        });
        grid.append(card);
      }
    } catch (err) {
      grid.append(el('div', { class: 'mh-amane-empty', text: err.message }));
    }
  }

  function renderDetail(detail) {
    const meta = detail.metadata;
    const files = detail.files || [];
    const back = el('button', { type: 'button', class: 'mh-amane-back', text: '← 返回新片速递' });
    back.addEventListener('click', () => {
      state.detail = null;
      render();
    });
    const form = el('form', { class: 'mh-amane-form' });
    const fields = [
      ['title', '标题', 'text'],
      ['studio', '片商', 'text'],
      ['publisher', '发行商', 'text'],
      ['series', '系列', 'text'],
      ['release', '发行日期', 'text'],
      ['plot', '剧情', 'textarea'],
    ];
    const inputs = {};
    for (const [key, label, type] of fields) {
      const input = type === 'textarea' ? el('textarea', { rows: 4 }) : el('input', { type: 'text' });
      input.value = meta[key] || '';
      inputs[key] = input;
      form.append(el('label', {}, [el('span', { text: label }), input]));
    }
    const actors = el('input', { type: 'text' });
    actors.value = (meta.actors || []).map((a) => a.name).join(', ');
    const tags = el('input', { type: 'text' });
    tags.value = (meta.tags || []).join(', ');
    inputs.actors = actors;
    inputs.tags = tags;
    form.append(el('label', {}, [el('span', { text: '出演' }), actors]));
    form.append(el('label', {}, [el('span', { text: '标签' }), tags]));

    const actions = el('div', { class: 'mh-amane-actions' });
    const save = el('button', { type: 'submit', class: 'mh-amane-primary', text: '保存' });
    actions.append(save);

    const rescan = el('button', { type: 'button', text: '重新刮削' });
    rescan.addEventListener('click', async () => {
      try {
        await request(api, '/api/tasks', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ kind: 'scrape', numbers: [meta.number] }),
        });
        toast('已入队重新刮削');
      } catch (err) {
        toast(err.message);
      }
    });

    const organize = el('button', { type: 'button', text: '重新整理' });
    organize.addEventListener('click', async () => {
      try {
        await request(api, '/api/tasks', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ kind: 'organize', numbers: [meta.number] }),
        });
        toast('已入队整理');
      } catch (err) {
        toast(err.message);
      }
    });
    actions.append(rescan, organize);

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const patch = {
        title: inputs.title.value,
        studio: inputs.studio.value,
        publisher: inputs.publisher.value,
        series: inputs.series.value,
        release: inputs.release.value,
        plot: inputs.plot.value,
        actors: inputs.actors.value.split(',').map((name) => ({ name: name.trim(), gender: 'unknown' })).filter((a) => a.name),
        tags: inputs.tags.value.split(',').map((tag) => tag.trim()).filter(Boolean),
      };
      try {
        const res = await request(api, `/api/metadata/${encodeURIComponent(meta.number)}`, {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(patch),
        });
        state.detail = { ...detail, metadata: res.metadata };
        toast('已保存');
        render();
      } catch (err) {
        toast(err.message);
      }
    });

    const fileList = el('div', { class: 'mh-amane-files' }, [el('h4', { text: '已索引文件' })]);
    if (!files.length) {
      fileList.append(el('div', { class: 'mh-amane-empty', text: '还没有关联文件；自动任务扫描下载库后会更新' }));
    }
    for (const file of files) {
      fileList.append(el('div', { class: 'mh-amane-file' }, [
        el('span', { text: file.path }),
        el('em', { text: [file.cd ? `CD${file.cd}` : '', file.definition, file.mosaic].filter(Boolean).join(' · ') }),
      ]));
    }

    body.append(
      back,
      el('h2', { class: 'mh-amane-detail-title', text: `${meta.number} ${meta.title || ''}` }),
      form,
      actions,
      fileList,
    );
  }

  async function renderTasks() {
    const list = el('div', { class: 'mh-amane-tasks' });
    body.append(list);

    async function refresh() {
      try {
        const data = await request(api, '/api/tasks?active=1&limit=20');
        list.innerHTML = '';
        if (!data.items.length) {
          list.append(el('div', { class: 'mh-amane-empty', text: '暂无进行中的任务' }));
          return;
        }
        for (const task of data.items) {
          const detail = task.progress || task.error || task.result || '';
          list.append(el('div', { class: 'mh-amane-task' }, [
            el('div', { class: 'mh-amane-task-line' }, [
              el('b', { text: TASK_KIND[task.kind] || task.kind }),
              el('span', { class: `mh-amane-status s-${task.status}`, text: TASK_STATUS[task.status] || task.status }),
            ]),
            el('p', { text: detail }),
          ]));
        }
      } catch (err) {
        list.innerHTML = '';
        list.append(el('div', { class: 'mh-amane-empty', text: err.message }));
      }
    }

    await refresh();
    activeTimer = setInterval(() => {
      refresh().catch(() => {});
    }, 2000);
  }

  async function renderSettings() {
    let cfg;
    let cfgSites = [];
    try {
      cfg = await request(api, '/api/config');
      const siteList = await request(api, '/api/sites');
      cfgSites = siteList.items || [];
    } catch (err) {
      body.append(el('div', { class: 'mh-amane-empty', text: err.message }));
      return;
    }

    const dirs = (cfg.download_dirs || []).map((d) => ({ path: d.path || '', mode: d.mode === 'move' ? 'move' : 'copy' }));
    const wrapper = el('div', { class: 'mh-amane-settings' });
    body.append(wrapper);

    // ---- 折叠分组 ----
    const groupMap = {};
    let openKey = 'general';
    function makeGroup(key, title) {
      const header = el('button', { type: 'button', class: 'mh-amane-group-head' }, [
        el('span', { text: title }),
        el('i', { class: 'mh-amane-chevron', text: '⌄' }),
      ]);
      const panel = el('div', { class: 'mh-amane-group-panel' });
      const node = el('div', { class: 'mh-amane-group' }, [header, panel]);
      header.addEventListener('click', () => {
        openKey = openKey === key ? null : key;
        syncGroups();
      });
      wrapper.append(node);
      groupMap[key] = { node };
      return panel;
    }
    function syncGroups() {
      for (const [key, item] of Object.entries(groupMap)) {
        item.node.classList.toggle('open', key === openKey);
      }
    }

    // ---- 常规设置 ----
    const generalPanel = makeGroup('general', '常规设置');
    const autoEnable = el('input', { type: 'checkbox' });
    autoEnable.checked = cfg.automation ? Boolean(cfg.automation.enabled) : true;
    const interval = el('input', { type: 'number', min: '5', max: '1440', step: '5' });
    interval.value = cfg.automation ? cfg.automation.interval_minutes : 30;
    const runOnStart = el('input', { type: 'checkbox' });
    runOnStart.checked = cfg.automation ? Boolean(cfg.automation.run_on_start) : true;
    generalPanel.append(
      el('label', { class: 'mh-amane-check' }, [autoEnable, el('span', { text: '自动扫描 → 刮削 → 整理' })]),
      el('label', { class: 'mh-amane-field' }, [
        el('span', { text: '执行间隔（分钟）' }),
        interval,
      ]),
      el('label', { class: 'mh-amane-check' }, [runOnStart, el('span', { text: '启动时执行一次' })]),
      el('p', { class: 'mh-amane-hint', text: '自动任务会遍历下载库，识别新番号后刮削并按整理方式入库。' }),
    );

    // ---- 网络 ----
    const networkPanel = makeGroup('network', '网络');
    const proxy = el('input', { type: 'checkbox' });
    proxy.checked = Boolean(cfg.use_mihomo_proxy);
    networkPanel.append(
      el('label', { class: 'mh-amane-check' }, [proxy, el('span', { text: '使用 mihomo 代理（127.0.0.1:7890）' })]),
      el('p', { class: 'mh-amane-hint', text: '需先在 Mica Home 里安装并启用 mihomo 模块；关闭即直连。' }),
    );

    // ---- 刮削站点 ----
    const sitePanel = makeGroup('sites', '刮削站点');
    const siteGrid = el('div', { class: 'mh-amane-site-grid' });
    const siteNodes = {};
    const names = (cfg.sites && cfg.sites.length ? cfg.sites : cfgSites.map((s) => s.name));
    for (const name of names) {
      const box = el('div', { class: 'mh-amane-site s-unknown', title: '未测试' }, [
        el('i', { class: 'mh-amane-site-block' }),
        el('span', { text: name }),
      ]);
      siteNodes[name] = box;
      siteGrid.append(box);
    }
    if (!names.length) siteGrid.append(el('div', { class: 'mh-amane-hint', text: '没有可用站点' }));
    const testBtn = el('button', { type: 'button', class: 'mh-amane-primary', text: '测试连通性' });
    testBtn.addEventListener('click', async () => {
      testBtn.disabled = true;
      for (const box of Object.values(siteNodes)) {
        box.className = 'mh-amane-site s-testing';
        box.title = '测试中…';
      }
      try {
        const res = await request(api, '/api/sites/check', { method: 'POST' });
        for (const item of res.items || []) {
          const box = siteNodes[item.name];
          if (!box) continue;
          const ok = item.status === 'ok';
          box.className = `mh-amane-site ${ok ? 's-ok' : 's-fail'}`;
          box.title = ok ? '通' : (item.detail || item.status);
        }
      } catch (err) {
        toast(err.message);
      } finally {
        testBtn.disabled = false;
      }
    });
    sitePanel.append(
      el('div', { class: 'mh-amane-site-row' }, [siteGrid, testBtn]),
      el('p', { class: 'mh-amane-hint', text: '绿色=网络通，红色=不通；点击按钮立即测试，走当前代理设置。' }),
    );

    // ---- 目录管理 ----
    const dirPanel = makeGroup('dirs', '目录管理');
    const rootInput = el('input', { type: 'text', value: cfg.library.root || '/media' });
    const rootBrowse = el('button', { type: 'button', text: '浏览' });
    rootBrowse.addEventListener('click', () => openPicker(rootInput.value.trim(), (picked) => {
      rootInput.value = picked;
    }));
    const dirList = el('div', { class: 'mh-amane-path-list' });
    const addDir = el('button', { type: 'button', text: '添加下载目录' });
    addDir.addEventListener('click', () => {
      dirs.push({ path: '', mode: 'copy' });
      refreshDirs();
    });
    dirPanel.append(
      el('div', { class: 'mh-amane-field' }, [
        el('span', { text: '片库路径' }),
        el('div', { class: 'mh-amane-path-row' }, [rootInput, rootBrowse]),
      ]),
      el('div', { class: 'mh-amane-field' }, [
        el('span', { text: '下载库路径' }),
        dirList,
      ]),
      addDir,
      el('p', { class: 'mh-amane-hint', text: '宿主 compose 把片库和下载目录挂进来后，这里才能浏览到。' }),
    );

    function refreshDirs() {
      dirList.innerHTML = '';
      if (!dirs.length) {
        dirList.append(el('div', { class: 'mh-amane-hint', text: '还没有下载目录；至少添加一个，自动任务才会工作。' }));
      }
      dirs.forEach((dir, idx) => {
        const input = el('input', { type: 'text', value: dir.path, placeholder: '/downloads' });
        input.addEventListener('input', () => {
          dir.path = input.value;
        });
        const browse = el('button', { type: 'button', text: '浏览' });
        browse.addEventListener('click', () => openPicker(input.value.trim(), (picked) => {
          input.value = picked;
          dir.path = picked;
        }));
        const remove = el('button', { type: 'button', class: 'mh-amane-danger', text: '删除' });
        remove.addEventListener('click', () => {
          dirs.splice(idx, 1);
          refreshDirs();
        });
        dirList.append(el('div', { class: 'mh-amane-path-row' }, [input, browse, remove]));
      });
      refreshModes();
    }

    // ---- 整理方式 ----
    const modePanel = makeGroup('modes', '整理方式');
    const modeList = el('div', { class: 'mh-amane-mode-list' });
    modePanel.append(
      modeList,
      el('p', { class: 'mh-amane-hint', text: 'PT 做种用复制（保留源文件）；迅雷等临时下载可移动。' }),
    );

    function refreshModes() {
      modeList.innerHTML = '';
      if (!dirs.length) {
        modeList.append(el('div', { class: 'mh-amane-hint', text: '先在「目录管理」添加下载目录。' }));
        return;
      }
      dirs.forEach((dir, idx) => {
        const row = el('div', { class: 'mh-amane-mode-row' });
        row.append(el('span', { class: 'mh-amane-mode-path', text: dir.path || '(未填写)' }));
        const radios = el('div', { class: 'mh-amane-radio-row' });
        for (const [value, label] of [['copy', '复制'], ['move', '移动']]) {
          const radio = el('input', { type: 'radio', name: `mh-amane-mode-${idx}` });
          radio.checked = dir.mode === value;
          radio.addEventListener('change', () => {
            if (radio.checked) dir.mode = value;
          });
          radios.append(el('label', { class: 'mh-amane-radio' }, [radio, el('span', { text: label })]));
        }
        row.append(radios);
        modeList.append(row);
      });
    }

    // ---- 视频路径模板 ----
    const tplPanel = makeGroup('template', '视频路径模板');
    const tplInput = el('input', { type: 'text', value: cfg.library.video_template || '{actress}/{number}[-CD{cd?}][-{sub?}].{ext}' });
    const chips = el('div', { class: 'mh-amane-chip-row' });
    for (const [token, desc] of PLACEHOLDERS) {
      const chip = el('button', { type: 'button', class: 'mh-amane-chip', text: token, title: desc });
      chip.addEventListener('click', () => {
        const pos = tplInput.selectionStart ?? tplInput.value.length;
        tplInput.value = tplInput.value.slice(0, pos) + token + tplInput.value.slice(pos);
        tplInput.focus();
      });
      chips.append(chip);
    }
    const nfo = el('input', { type: 'checkbox' });
    nfo.checked = cfg.library.write_nfo !== false;
    const images = el('input', { type: 'checkbox' });
    images.checked = cfg.library.write_images !== false;
    tplPanel.append(
      el('div', { class: 'mh-amane-field' }, [el('span', { text: '模板（只写相对片库根的部分，片库路径自动拼接）' }), tplInput]),
      el('p', { class: 'mh-amane-hint', text: '语法：可选组 [..] 在组内占位符为空时整组省略；双层 [[..]]；值映射 {mosaic?|censored=有码,uncensored=无码}。' }),
      chips,
      el('label', { class: 'mh-amane-check' }, [nfo, el('span', { text: '写出 NFO' })]),
      el('label', { class: 'mh-amane-check' }, [images, el('span', { text: '写出图片' })]),
    );

    const save = el('button', { type: 'button', class: 'mh-amane-primary mh-amane-save', text: '保存设置' });
    save.addEventListener('click', async () => {
      const next = {
        addr: cfg.addr,
        use_mihomo_proxy: proxy.checked,
        sites: cfg.sites || [],
        site_settings: cfg.site_settings || {},
        library: {
          root: rootInput.value.trim(),
          video_template: tplInput.value.trim(),
          write_nfo: nfo.checked,
          write_images: images.checked,
        },
        download_dirs: dirs
          .map((d) => ({ path: d.path.trim(), mode: d.mode }))
          .filter((d) => d.path),
        automation: {
          enabled: autoEnable.checked,
          interval_minutes: Number(interval.value) || 30,
          run_on_start: runOnStart.checked,
        },
      };
      try {
        await request(api, '/api/config', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(next),
        });
        toast('设置已保存');
      } catch (err) {
        toast(err.message);
      }
    });
    body.append(save);

    refreshDirs();
    syncGroups();

    // ---- 文件浏览器 ----
    function openPicker(start, onPick) {
      const overlay = el('div', { class: 'mh-amane-picker' });
      const panel = el('div', { class: 'mh-amane-picker-panel' });
      const rootBar = el('div', { class: 'mh-amane-rootbar' });
      const crumbs = el('div', { class: 'mh-amane-crumbs' });
      const list = el('div', { class: 'mh-amane-dirlist' });
      const select = el('button', { type: 'button', class: 'mh-amane-primary', text: '选择此目录' });
      const cancel = el('button', { type: 'button', text: '取消' });
      let current = '';

      async function load(target) {
        list.innerHTML = '';
        list.append(el('div', { class: 'mh-amane-hint', text: '加载中…' }));
        try {
          const data = await request(api, `/api/fs?path=${encodeURIComponent(target || '')}`);
          current = data.path || '';
          rootBar.innerHTML = '';
          for (const r of data.roots || []) {
            const b = el('button', { type: 'button', text: r.split('/').filter(Boolean).pop() || r });
            b.addEventListener('click', () => load(r));
            rootBar.append(b);
          }
          renderCrumbs(data.roots || []);
          list.innerHTML = '';
          if (data.parent) {
            const up = el('button', { type: 'button', class: 'mh-amane-diritem', text: '..（上一级）' });
            up.addEventListener('click', () => load(data.parent));
            list.append(up);
          }
          for (const entry of data.entries || []) {
            const item = el('button', { type: 'button', class: 'mh-amane-diritem', text: entry.name });
            item.addEventListener('click', () => load(entry.path));
            list.append(item);
          }
          if (!(data.entries || []).length && !data.parent) {
            list.append(el('div', { class: 'mh-amane-hint', text: '这个目录是空的' }));
          }
          select.disabled = !current;
        } catch (err) {
          list.innerHTML = '';
          list.append(el('div', { class: 'mh-amane-hint', text: err.message }));
        }
      }

      function renderCrumbs(roots) {
        crumbs.innerHTML = '';
        if (!current) {
          crumbs.append(el('span', { class: 'mh-amane-hint', text: '请选择一个根目录' }));
          return;
        }
        const root = roots.find((r) => current === r || current.startsWith(`${r}/`));
        if (!root) return;
        const nodes = [{ label: root.split('/').filter(Boolean).pop() || root, path: root }];
        let acc = root;
        for (const seg of current.slice(root.length).split('/').filter(Boolean)) {
          acc += `/${seg}`;
          nodes.push({ label: seg, path: acc });
        }
        nodes.forEach((node, idx) => {
          const b = el('button', { type: 'button', text: node.label });
          b.addEventListener('click', () => load(node.path));
          crumbs.append(b);
          if (idx < nodes.length - 1) crumbs.append(el('span', { class: 'mh-amane-sep', text: '/' }));
        });
      }

      select.addEventListener('click', () => {
        if (!current) return;
        onPick(current);
        close();
      });
      cancel.addEventListener('click', close);
      overlay.addEventListener('click', (e) => {
        if (e.target === overlay) close();
      });
      function close() {
        overlay.remove();
      }

      panel.append(el('h3', { text: '选择目录' }), rootBar, crumbs, list,
        el('div', { class: 'mh-amane-picker-actions' }, [cancel, select]));
      overlay.append(panel);
      document.body.append(overlay);
      load(start || '');
    }
  }

  render();
  return () => {
    stopTimer();
    root.innerHTML = '';
    root.classList.remove('mh-amane');
  };
}

export async function renderCard(root, api) {
  injectStyle();
  root.innerHTML = '';
  root.classList.add('mh-amane');
  try {
    const stats = await request(api, '/api/stats');
    root.append(el('div', { class: 'mh-amane-card-stats' }, [
      el('strong', { text: String(stats.counts.metadata ?? 0) }),
      el('span', { text: '影片' }),
      el('strong', { text: String(stats.counts.tasks ?? 0) }),
      el('span', { text: '任务中' }),
      el('strong', { text: String(stats.counts.files ?? 0) }),
      el('span', { text: '文件' }),
    ]));
  } catch (err) {
    root.append(el('div', { class: 'mh-amane-empty', text: '服务未就绪' }));
  }
  return () => {
    root.innerHTML = '';
  };
}
