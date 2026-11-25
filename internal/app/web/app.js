const state = { devices: [], accounts: [], discovery: [], logs: [], page: 'overview', liveRevision: 0, xiaomiChallengeId: '', xiaomiChallengeType: '', xiaomiVerifyFlag: 0 };
const pendingDeviceCommands = new Set();
const $ = (s, root=document) => root.querySelector(s);
const $$ = (s, root=document) => [...root.querySelectorAll(s)];
// Older data files or empty Go slices may decode as null; keep UI collections safe.
const asArray = value => Array.isArray(value) ? value : [];
// Each browser tab owns an independent lease. Hidden/closed tabs release it;
// the server stops all provider polling when no visible authenticated tab remains.
const stateSyncClientId = (() => {
  const random = () => Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
  const uuid = globalThis.crypto?.randomUUID?.();
  return `web-${uuid || random()}-${Date.now().toString(36)}`;
})();
let stateSyncHeartbeat = null;
let liveDeviceTimer = null;
let liveDeviceBusy = false;
let stateSyncRequest = null;
let queuedStateSync = null;
async function sendStateSyncActivity(active, keepalive=false) {
  // Serialize changes but never drop a release request. Previously, a page
  // becoming hidden while an active heartbeat was in flight could lose the
  // `active:false` update and keep cloud polling alive until lease expiry.
  queuedStateSync = { active, keepalive };
  if (stateSyncRequest) return stateSyncRequest;
  stateSyncRequest = (async () => {
    while (queuedStateSync) {
      const job = queuedStateSync;
      queuedStateSync = null;
      try {
        await fetch('/api/state-sync/activity', {
          method: 'POST', credentials: 'same-origin', cache: 'no-store', keepalive: job.keepalive,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ clientId: stateSyncClientId, active: job.active })
        });
      } catch (_) {
        // The server lease expires automatically if this tab disappears.
      }
    }
  })();
  try {
    await stateSyncRequest;
  } finally {
    stateSyncRequest = null;
    if (queuedStateSync) void sendStateSyncActivity(queuedStateSync.active, queuedStateSync.keepalive);
  }
}
function shouldSyncProviderStates() {
  return document.visibilityState === 'visible' && document.body.classList.contains('authenticated') && ['overview', 'devices'].includes(state.page);
}
function startLiveDeviceUpdates() {
  if (!shouldSyncProviderStates()) return;
  if (liveDeviceTimer) clearInterval(liveDeviceTimer);
  // Lightweight AJAX: only the live state delta is fetched. Account, logs,
  // discovery and full device control schemas are not reloaded every tick.
  pollLiveDeviceStates(true);
  liveDeviceTimer = setInterval(() => {
    if (shouldSyncProviderStates()) pollLiveDeviceStates();
  }, 2000);
}
function stopLiveDeviceUpdates() {
  if (liveDeviceTimer) clearInterval(liveDeviceTimer);
  liveDeviceTimer = null;
}
function startStateSyncActivity() {
  if (!shouldSyncProviderStates()) return;
  sendStateSyncActivity(true);
  startLiveDeviceUpdates();
  if (stateSyncHeartbeat) clearInterval(stateSyncHeartbeat);
  stateSyncHeartbeat = setInterval(() => {
    if (shouldSyncProviderStates()) sendStateSyncActivity(true);
  }, 6000);
}
function stopStateSyncActivity() {
  if (stateSyncHeartbeat) clearInterval(stateSyncHeartbeat);
  stateSyncHeartbeat = null;
  stopLiveDeviceUpdates();
  sendStateSyncActivity(false, true);
}

function showLoginScreen(message='') {
  stopStateSyncActivity();
  document.body.classList.remove('authenticated');
  $('#login-screen').hidden = false;
  if (message) {
    $('#login-error').textContent = message;
    $('#login-error').hidden = false;
  }
}
function enterApplication() {
  $('#login-screen').hidden = true;
  document.body.classList.add('authenticated');
  startStateSyncActivity();
}
async function api(path, options={}) {
  const opts = { ...options, credentials: 'same-origin', cache: 'no-store', headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } };
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && path !== '/api/auth/login') showLoginScreen(data.error || '登录状态已失效，请重新登录');
  if (!res.ok) throw new Error(data.error || `请求失败 (${res.status})`);
  return data;
}
function esc(v='') { return String(v).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function toast(message, type='') {
  const el = document.createElement('div'); el.className = `toast ${type}`; el.textContent = message;
  $('#toast-stack').appendChild(el); setTimeout(() => el.remove(), 3800);
}
function fmtTime(value) {
  if (!value) return '—'; const d = new Date(value); if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString('zh-CN', { month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', hour12:false });
}
function timeAgo(value) {
  if (!value) return '时间未知'; const sec = Math.max(0, Math.floor((Date.now()-new Date(value).getTime())/1000));
  if (sec < 60) return '刚刚'; if (sec < 3600) return `${Math.floor(sec/60)} 分钟前`; if (sec < 86400) return `${Math.floor(sec/3600)} 小时前`; return fmtTime(value);
}
function glyph(category) {
  const map = { '插座':['glyph-plug','ϟ'], '开关':['glyph-switch','◈'], '灯光':['glyph-light','☼'], '传感器':['glyph-sensor','⌁'], '温湿度计':['glyph-sensor','⌁'], '摄像头':['glyph-camera','▣'], '音箱':['glyph-speaker','◖'], '空调':['glyph-climate','❄'], '扫地机器人':['glyph-robot','◉'], '风扇':['glyph-fan','✣'], '影音设备':['glyph-media','▣'], '洗碗机':['glyph-appliance','▤'], '冰箱':['glyph-appliance','▤'], '加湿器':['glyph-climate','♨'], '空气净化器':['glyph-climate','≋'], '智能门锁':['glyph-switch','▣'], '窗帘':['glyph-switch','▥'], '穿戴设备':['glyph-sensor','◉'], '网络设备':['glyph-media','⌁'] };
  return map[category] || ['glyph-other','◇'];
}
function formatMetric(value, digits=2) {
  const n = Number(value);
  return Number.isFinite(n) ? n.toFixed(digits).replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1') : '—';
}
function renderEnergyMetrics(energy) {
  if (!energy || typeof energy !== 'object') return '';
  const metrics = [
    ['实时功率', energy.powerW, 'W', 1],
    ['电压', energy.voltageV, 'V', 1],
    ['电流', energy.currentA, 'A', 2],
    ['今日用电', energy.todayKWh, 'kWh', 3],
    ['本周用电', energy.weekKWh, 'kWh', 3],
    ['本月用电', energy.monthKWh, 'kWh', 3],
    ['累计用电', energy.totalKWh, 'kWh', 3],
  ].filter(([, value]) => value !== null && value !== undefined && Number.isFinite(Number(value)));
  const source = energy.source ? `<span>${esc(energy.source)}</span>` : '';
  const updated = energy.updatedAt ? `<span>更新 ${fmtTime(energy.updatedAt)}</span>` : '';
  // Do not display empty placeholders. A meter capability alone is not a
  // reading; only values returned by the provider or locally accumulated from
  // actual samples are shown. Numeric zero remains a valid reading.
  if (!metrics.length) return '';
  const hasPeriodMetric = metrics.some(([label]) => label === '今日用电' || label === '本周用电');
  const periodHint = hasPeriodMetric ? '<span>日统计按本机时区 · 周统计从周一累计</span>' : '';
  return `<section class="energy-panel"><div class="energy-head"><strong>用电统计</strong><span class="energy-badge">${source || '计量数据'}</span></div><div class="energy-grid">${metrics.map(([label,value,unit,digits]) => { const hint = label === '今日用电' ? energy.todaySource : label === '本周用电' ? energy.weekSource : ''; return `<div class="energy-metric ${label === '今日用电' || label === '本周用电' ? 'energy-period-metric' : ''}"><small>${label}</small><strong>${formatMetric(value,digits)} <em>${unit}</em></strong>${hint ? `<small class="energy-source-hint">${esc(hint)}</small>` : ''}</div>`; }).join('')}</div>${updated || periodHint ? `<div class="energy-foot">${updated}${periodHint}</div>` : ''}</section>`;
}

function deviceDisplayAttributes(d) {
  const attrs = asArray(d?.attributes).filter(a => a && a.label && a.value !== undefined && a.value !== null).map(a => ({...a}));
  if (String(d?.model || '').trim().toLowerCase() !== 'chuangmi.plug.m3') return attrs;
  // Old data may contain two rows both named "开关状态" because switch.on
  // and indicator-light.on were conflated. Only Device.power (read from 2/1)
  // may generate the main relay row; keep the LED as a separate indicator.
  const filtered = [];
  for (const attr of attrs) {
    const key = String(attr.key || '').trim().toLowerCase();
    if (key === 'indicator_light' || key === 'service_on_3_1') {
      attr.label = '指示灯';
      filtered.push(attr);
      continue;
    }
    if (key === 'on' || ['开关状态','电源开关'].includes(String(attr.label || '').trim())) continue;
    filtered.push(attr);
  }
  if (typeof d.power === 'boolean') {
    filtered.unshift({key:'on', label:'开关状态', value:d.power ? '开启' : '关闭'});
  }
  return filtered;
}
function renderDeviceAttributes(d) {
  const attrs = deviceDisplayAttributes(d).slice(0, 8);
  if (!attrs.length) return '';
  return `<section class="device-attributes"><span class="device-attributes-head">设备状态</span><div class="device-attributes-grid">${attrs.map(a => `<span class="device-attribute"><small>${esc(a.label)}</small><strong>${esc(a.value)}${a.unit ? `<em>${esc(a.unit)}</em>` : ''}</strong></span>`).join('')}</div></section>`;
}
function deviceFallbackSummary(d) {
  const category = d.category || '设备';
  if (d.energy?.supported) return d.online ? '计量功能已识别 · 等待读数' : '设备离线 · 暂无读数';
  const label = ({'摄像头':'摄像头已接入','音箱':'音箱已连接','温湿度计':'环境传感器在线','传感器':'传感器在线','插座':'插座已接入','灯光':'灯具已接入','空调':'空调已接入','扫地机器人':'清洁设备已接入','影音设备':'影音设备已接入','洗碗机':'家电已接入'})[category] || '设备已接入';
  return d.online ? label : `${category}离线`;
}
function renderDeviceHoverActions(d) {
  return `<div class="device-hover-actions" aria-label="设备操作"><button type="button" class="device-api-copy" data-copy-device-api="${esc(d.id)}" title="复制设备 API"><span aria-hidden="true">⧉</span><span>复制 API</span></button><button type="button" class="device-delete" data-delete-device="${esc(d.id)}" title="删除设备" aria-label="删除设备"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3 4.5h10M6 2.5h4M4.5 4.5l.6 9h5.8l.6-9M6.5 6.5v5M9.5 6.5v5"/></svg></button></div>`;
}
function overviewEnergy(d) {
  const energy = d.energy;
  if (!energy || !energy.supported) return '';
  const items = [
    ['实时功率', energy.powerW, 'W', 1, ''],
    ['今日用电', energy.todayKWh, 'kWh', 3, energy.todaySource || ''],
    ['本周用电', energy.weekKWh, 'kWh', 3, energy.weekSource || ''],
  ].filter(([, value]) => value !== null && value !== undefined && Number.isFinite(Number(value)));
  if (!items.length) return '';
  return `<div class="overview-energy">${items.map(([label,value,unit,digits,source]) => `<div><small>${label}</small><strong>${formatMetric(value,digits)} <em>${unit}</em></strong>${source ? `<small class="overview-energy-source">${esc(source)}</small>` : ''}</div>`).join('')}</div><div class="overview-energy-updated">计量更新 ${fmtTime(energy.updatedAt)}</div>`;
}
function devicePowerSummary(d, channels=asArray(d.channels)) {
  if (channels.length > 1) return `${channels.filter(ch => ch.power === true).length}/${channels.length} 路开启`;
  if (d.power === true) return '已开启';
  if (d.power === false) return '已关闭';
  if (d.canControl || String(d?.model || '').trim().toLowerCase() === 'chuangmi.plug.m3') return '开关状态未知';
  return d.value || deviceFallbackSummary(d);
}
function isPrimaryMIoTPowerControl(d, c) {
  if (!c || c.kind !== 'property' || !['bool','boolean'].includes(String(c.format || '').toLowerCase())) return false;
  if (String(d?.model || '').toLowerCase() === 'chuangmi.plug.m3') return Number(c.siid) === 2 && Number(c.piid) === 1;
  return miotControlLabel(c.name) === '电源开关';
}
function miotControlLabelForDevice(d, c) {
  if (String(d?.model || '').toLowerCase() === 'chuangmi.plug.m3' && Number(c?.siid) === 3 && Number(c?.piid) === 1) return '指示灯';
  return miotControlLabel(c?.name);
}
function isDisplayOnlyMIoTProperty(d, c) {
  // The plug's indicator LED is useful telemetry but should not render a
  // second on/off button beside the primary relay switch.
  return c?.kind === 'property'
    && String(d?.model || '').trim().toLowerCase() === 'chuangmi.plug.m3'
    && Number(c?.siid) === 3 && Number(c?.piid) === 1;
}
function renderDeviceCard(d) {
  const [cls, icon] = glyph(d.category);
  const online = d.online ? '<span class="device-status">● 在线</span>' : '<span class="device-status offline">● 离线</span>';
  const channels = asArray(d.channels);
  const controls = asArray(d.controls).filter(c => c && !isPrimaryMIoTPowerControl(d, c) && !isDisplayOnlyMIoTProperty(d, c) && ((c.kind === 'property' && miotControlLabelForDevice(d, c)) || (c.kind === 'action' && isSafeVisibleMIoTAction(c))));
  const controlCount = controls.length + channels.length + (d.canControl && !channels.length ? 1 : 0);
  const hasControls = controlCount > 0 && d.canControl || controls.length > 0;
  const summary = devicePowerSummary(d, channels);
  const control = renderDeviceControls(d);
  const controlHTML = hasControls && control
    ? `<details class="overview-control-details"><summary><strong>设备控制</strong><span>${controlCount} 项可用</span></summary><div class="overview-control-body">${control}</div></details>`
    : (d.energy?.supported || asArray(d.attributes).length) ? '<span class="overview-readonly">实时监测 · 只读设备</span>' : '';
  const attrs = deviceDisplayAttributes(d).slice(0, 2);
  return `<article class="device-card ${controlCount >= 5 ? 'has-rich-controls' : ''}">
    <div class="overview-device-head">
      <div class="overview-device-identity"><div class="device-glyph ${cls}">${icon}</div><div class="overview-device-title"><h3 title="${esc(d.name)}">${esc(d.name)}</h3><small>${esc(deviceBrandGroup(d))} · ${esc(d.room || '未分区')}</small></div></div>
      <div class="overview-device-tools">${online}${renderDeviceHoverActions(d)}</div>
    </div>
    <div class="overview-device-meta"><span>${esc(d.category || '其他')}</span><span>${esc(summary)}</span></div>
    ${attrs.length ? `<div class="overview-attributes">${attrs.map(a => `<span>${esc(a.label)} <b>${esc(a.value)}${esc(a.unit || '')}</b></span>`).join('')}</div>` : ''}
    ${overviewEnergy(d)}${controlHTML}
  </article>`;
}
function deviceBrandGroup(d) {
  if (d.providerGroup) return d.providerGroup;
  if (d.protocol === 'xiaomi-cloud') return '小米 / 米家';
  if (d.protocol === 'ewelink-cloud') return '易微联 / eWeLink';
  if (d.protocol === 'esp32-mqtt' || d.protocol === 'esp32-http') return 'ESP32';
  return d.brand || '其他设备';
}
function miotControlLabel(name) {
  const raw = String(name || '').trim();
  const key = raw.toLowerCase().replace(/[^a-z0-9]+/g, '');
  const labels = {
    on:'电源开关', power:'电源开关', switch:'电源开关', relaystate:'电源开关', switchstate:'电源开关',
    volume:'音量', mute:'静音', isvolumemuted:'静音', sleepmode:'睡眠模式', mode:'工作模式',
    nightshot:'夜视模式', imagerollover:'图像翻转', timewatermark:'时间水印',
    motiontracking:'移动跟踪', humantracking:'人形跟踪', motiondetection:'移动侦测', humandetection:'人形检测',
    detectionsensitivity:'侦测灵敏度', motiondetectionsensitivity:'移动侦测灵敏度', recordingmode:'录像模式',
    targettemperature:'目标温度', fanlevel:'风速档位', verticalswing:'上下摆风', brightness:'亮度',
    suctionlevel:'吸力档位', sweeptype:'清扫类型', battery:'电池电量',
    manualsoundandlightwarning:'声光警报', alarm:'报警', childlock:'童锁',
  };
  if (labels[key]) return labels[key];
  if (!raw || ['设备操作','property','unknown'].includes(raw.toLowerCase())) return '';
  return raw.replace(/[-_]+/g, ' ').replace(/\b[a-z]/g, c => c.toUpperCase());
}
function miotControlRank(c) {
  const key = String(c?.name || '').toLowerCase().replace(/[^a-z0-9]+/g, '');
  const ranks = ['on','power','switch','relaystate','switchstate','mode','volume','mute','nightshot','recordingmode','motiondetection','humandetection','detectionsensitivity','targettemperature','fanlevel','brightness','suctionlevel','sweeptype','verticalswing','sleepmode'];
  const index = ranks.indexOf(key);
  return index < 0 ? 100 : index;
}
function isSafeVisibleMIoTAction(c) {
  const name = `${c?.name || ''} ${c?.description || ''}`.toLowerCase();
  if (!name.trim()) return false;
  if (/delete|\bdel\b|reset|format|unbind|erase|factory|restart|calibrat|clear|remove|upload video|sync manual/.test(name)) return false;
  return !['设备操作','unknown','action'].includes(String(c.name || '').trim().toLowerCase());
}
function deviceCommandPending(id, channel) {
  return pendingDeviceCommands.has(`${id}:${channel === undefined || channel === null ? 'default' : Number(channel)}`);
}
function renderDeviceControls(d) {
  const channels = asArray(d.channels);
  const hasPower = d.power !== null && d.power !== undefined;
  const controls = asArray(d.controls);
  const propertyControls = controls.filter(c => c && c.kind === 'property'
    && !isPrimaryMIoTPowerControl(d, c)
    && !isDisplayOnlyMIoTProperty(d, c)
    && miotControlLabelForDevice(d, c));
  const actionControls = controls.filter(c => c && c.kind === 'action' && isSafeVisibleMIoTAction(c));
  const simpleProperties = propertyControls.filter(c => {
    const format = String(c.format || '').toLowerCase();
    return ['bool','boolean','string'].includes(format) || /^(u?int|float|double)/.test(format) || asArray(c.options).length > 0;
  }).sort((a,b) => miotControlRank(a)-miotControlRank(b));
  const sortedActions = actionControls.slice().sort((a,b) => {
    const rank = name => ({'start sweep':0,'start only sweep':1,'start sweep mop':2,'start mop':3,'start room sweep':4,'start charge':5,'play':0,'pause':1,'next':2,'previous':3,'stop alarm':4}[String(name||'').toLowerCase()] ?? 100);
    return rank(a.name)-rank(b.name);
  });
  let powerHTML = '';
  if (channels.length > 1 && d.canControl) {
    powerHTML = `<div class="device-channel-controls">${channels.map(ch => `<div class="device-channel-row"><span>${esc(ch.name || `通道 ${Number(ch.outlet)+1}`)}</span><button class="switch ${ch.power ? 'on' : ''}"${deviceCommandPending(d.id, Number(ch.outlet)) ? ' disabled' : ''} data-command="toggle" data-channel="${Number(ch.outlet)}" data-id="${esc(d.id)}" aria-label="切换${esc(d.name)} ${esc(ch.name || `通道 ${Number(ch.outlet)+1}`)}"></button></div>`).join('')}</div>`;
  } else if (d.canControl && hasPower) {
    const powerChannel = channels.length === 1 ? Number(channels[0].outlet) : undefined;
    const channel = powerChannel === undefined ? '' : ` data-channel="${powerChannel}"`;
    powerHTML = `<button class="switch ${d.power ? 'on' : ''}"${deviceCommandPending(d.id, powerChannel) ? ' disabled' : ''} data-command="toggle"${channel} data-id="${esc(d.id)}" aria-label="切换${esc(d.name)}"></button>`;
  } else if (d.canControl) {
    // Unknown state must not be rendered as ON or guessed by a toggle.
    // Explicit ON/OFF controls remain safe while the next cloud read is pending.
    powerHTML = `<div class="miot-control-buttons power-state-unknown"><button type="button" class="miot-action-btn"${deviceCommandPending(d.id) ? ' disabled' : ''} data-command="turn_on" data-id="${esc(d.id)}">开启</button><button type="button" class="miot-action-btn"${deviceCommandPending(d.id) ? ' disabled' : ''} data-command="turn_off" data-id="${esc(d.id)}">关闭</button></div>`;
  }

  const propertyHTML = c => {
    const format = String(c.format || '').toLowerCase();
    const label = miotControlLabelForDevice(d, c);
    if (['bool','boolean'].includes(format)) {
      const known = typeof c.value === 'boolean';
      const value = known ? c.value : false;
      return `<div class="miot-control-item"><span>${esc(label)}</span><div class="miot-control-buttons">${known
        ? `<button type="button" class="miot-action-btn ${value ? 'active' : ''}" data-miot-property data-id="${esc(d.id)}" data-siid="${Number(c.siid)}" data-piid="${Number(c.piid)}" data-value="${value ? 'false' : 'true'}">${value ? '关闭' : '开启'}</button>`
        : `<button type="button" class="miot-action-btn" data-miot-property data-id="${esc(d.id)}" data-siid="${Number(c.siid)}" data-piid="${Number(c.piid)}" data-value="true">开启</button><button type="button" class="miot-action-btn" data-miot-property data-id="${esc(d.id)}" data-siid="${Number(c.siid)}" data-piid="${Number(c.piid)}" data-value="false">关闭</button>`}</div></div>`;
    }
    if (asArray(c.options).length) {
      return `<label class="miot-control-item"><span>${esc(label)}</span><select class="miot-control-select" data-id="${esc(d.id)}" data-siid="${Number(c.siid)}" data-piid="${Number(c.piid)}">${asArray(c.options).map(option => `<option value="${esc(JSON.stringify(option.value))}" ${String(option.value) === String(c.value) ? 'selected' : ''}>${esc(option.label)}</option>`).join('')}</select></label>`;
    }
    const inputID = `miot-value-${String(d.id).replace(/[^a-zA-Z0-9_-]/g,'-')}-${Number(c.siid)}-${Number(c.piid)}`;
    const numeric = /^(u?int|float|double)/.test(format);
    const inputType = numeric ? 'number' : 'text';
    const step = numeric && !/^(u?int)/.test(format) ? 'any' : '1';
    const value = c.value === undefined || c.value === null ? '' : String(c.value);
    return `<label class="miot-control-item miot-control-value"><span>${esc(label)}</span><input id="${esc(inputID)}" class="miot-value-input" type="${inputType}" step="${step}" value="${esc(value)}" placeholder="输入值"><button type="button" class="miot-action-btn" data-miot-set-property data-id="${esc(d.id)}" data-siid="${Number(c.siid)}" data-piid="${Number(c.piid)}" data-format="${esc(format)}" data-input-id="${esc(inputID)}">设置</button></label>`;
  };
  const actionHTML = c => `<button type="button" class="miot-action-btn" data-miot-action data-id="${esc(d.id)}" data-siid="${Number(c.siid)}" data-aiid="${Number(c.aiid)}" data-control="${esc(c.id)}" title="${esc(c.description || c.name)}">${esc(({ 'Start Sweep':'开始清扫','Stop Sweeping':'停止清扫','Start Only Sweep':'仅扫地','Start Sweep Mop':'扫拖','Start Mop':'开始拖地','Start Room Sweep':'分区清扫','Start Charge':'返回充电座','Play':'播放','Pause':'暂停','Next':'下一曲','Previous':'上一曲','Stop Alarm':'停止闹钟' })[c.name] || c.name)}</button>`;

  const primaryProperties = simpleProperties.filter(c => miotControlRank(c) < 100).slice(0, 4);
  const usedPropertyIDs = new Set(primaryProperties.map(c => `${c.siid}:${c.piid}`));
  const advancedProperties = simpleProperties.filter(c => !usedPropertyIDs.has(`${c.siid}:${c.piid}`));
  const primaryActions = sortedActions.filter(c => /^(start sweep|start only sweep|start sweep mop|start mop|start room sweep|start charge|play|pause|next|previous|stop alarm)$/i.test(c.name)).slice(0, 4);
  const usedActionIDs = new Set(primaryActions.map(c => `${c.siid}:${c.aiid}`));
  const advancedActions = sortedActions.filter(c => !usedActionIDs.has(`${c.siid}:${c.aiid}`));
  const totalControls = simpleProperties.length + sortedActions.length + (channels.length > 1 ? channels.length : 0);
  const rich = totalControls >= 5 || channels.length >= 4;

  if (!powerHTML && simpleProperties.length === 0 && sortedActions.length === 0) {
    return `<span class="device-readonly">${asArray(d.attributes).length || d.energy?.supported ? '实时监测' : '云端状态'}</span>`;
  }
  if (!rich) {
    return `${powerHTML}${simpleProperties.map(propertyHTML).join('')}${sortedActions.map(actionHTML).join('')}`;
  }
  const primary = [...primaryProperties.map(propertyHTML), ...primaryActions.map(actionHTML)].join('');
  const advanced = [...advancedProperties.map(propertyHTML), ...advancedActions.map(actionHTML)].join('');
  return `<section class="device-control-card"><header class="device-control-card-head"><div><strong>设备控制</strong><small>${totalControls} 项可用设置</small></div><span>常用功能</span></header>${powerHTML ? `<div class="device-control-power">${powerHTML}</div>` : ''}<div class="device-control-primary">${primary || '<span class="device-readonly">没有常用快捷控制</span>'}</div>${advanced ? `<details class="device-control-advanced"><summary>更多设置与动作 <span>${advancedProperties.length + advancedActions.length} 项</span></summary><div class="device-control-advanced-body"><div class="miot-control-list">${advancedProperties.map(propertyHTML).join('')}</div><div class="miot-action-list">${advancedActions.map(actionHTML).join('')}</div></div></details>` : ''}</section>`;
}

function buildDeviceAPIText(d) {
  const id = String(d.id || '');
  const base = 'http://<GoHome主机地址>:17880';
  const jar = 'gohome-cookie.txt';
  const lines = [
    `# ${d.name || '设备'} API`,
    `# Device ID: ${id}`,
    `# 首次使用时执行登录命令；Cookie 会保存到本地文件，后续请求复用。`,
    `AUTH_JAR=${jar}`,
    `curl -sS -c "$AUTH_JAR" -b "$AUTH_JAR" -X POST '${base}/api/auth/login' -H 'Content-Type: application/json' -d '{"username":"admin","password":"123456"}'`,
    ''
  ];
  lines.push('# 查询设备详情');
  lines.push(`curl -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}'`);
  if (d.protocol === 'xiaomi-cloud' && asArray(d.controls).length) {
    const controls = asArray(d.controls);
    const power = controls.find(c => c.kind === 'property' && c.name === '电源开关' && ['bool','boolean'].includes(String(c.format || '').toLowerCase()));
    if (power) {
      lines.push('', '# 电源开关');
      lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/command' -H 'Content-Type: application/json' -d '{"command":"turn_on"}'`);
      lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/command' -H 'Content-Type: application/json' -d '{"command":"turn_off"}'`);
    }
    for (const c of controls.filter(x => x.kind === 'property')) {
      const format = String(c.format || '').toLowerCase();
      let example = c.options?.length ? JSON.stringify(c.options[0].value) : ['bool','boolean'].includes(format) ? 'true' : /^(u?int|float|double)/i.test(format) ? '0' : '""';
      lines.push('', `# 属性：${c.name}（SIID=${c.siid} PIID=${c.piid} FORMAT=${c.format || 'unknown'}）`);
      lines.push(`# 将 value 替换成设备规格允许的值`);
      lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/property' -H 'Content-Type: application/json' -d '${JSON.stringify({siid:c.siid,piid:c.piid,value:JSON.parse(example)})}'`);
    }
    for (const c of controls.filter(x => x.kind === 'action')) {
      const values = asArray(c.inputs).map(input => input.options?.length ? input.options[0].value : /^(u?int|float|double)/i.test(input.format || '') ? 0 : /^(bool|boolean)$/i.test(input.format || '') ? false : '');
      lines.push('', `# 动作：${c.name}（SIID=${c.siid} AIID=${c.aiid}，参数数量=${values.length}）`);
      lines.push(`# ${c.description || '执行 MIoT 动作'}`);
      lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/action' -H 'Content-Type: application/json' -d '${JSON.stringify({siid:c.siid,aiid:c.aiid,params:values})}'`);
    }
  } else if (d.protocol === 'ewelink-cloud' && d.canControl) {
    lines.push('', '# 开关控制');
    if (asArray(d.channels).length > 1) {
      for (const channel of asArray(d.channels)) {
        lines.push(`# ${channel.name || `通道 ${Number(channel.outlet)+1}`}`);
        lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/command' -H 'Content-Type: application/json' -d '${JSON.stringify({command:'turn_on',channel:Number(channel.outlet)})}'`);
        lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/command' -H 'Content-Type: application/json' -d '${JSON.stringify({command:'turn_off',channel:Number(channel.outlet)})}'`);
      }
    } else {
      lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/command' -H 'Content-Type: application/json' -d '{"command":"turn_on"}'`);
      lines.push(`curl -X POST -b "$AUTH_JAR" '${base}/api/v1/devices/${encodeURIComponent(id)}/command' -H 'Content-Type: application/json' -d '{"command":"turn_off"}'`);
    }
  } else {
    lines.push('', '# 当前设备没有发现可执行控制能力；重新同步后会自动更新可用 API。');
  }
  return lines.join('\n');
}

function renderDeviceRow(d) {
  const [cls, icon] = glyph(d.category);
  const status = d.online ? '<span class="device-status">● 在线</span>' : '<span class="device-status offline">● 离线</span>';
  const channels = asArray(d.channels);
  const summary = devicePowerSummary(d, channels);
  const richControls = channels.length >= 4 || asArray(d.controls).filter(c => c && ((c.kind === 'property' && miotControlLabel(c.name)) || (c.kind === 'action' && isSafeVisibleMIoTAction(c)))).length + channels.length >= 5;
  return `<article class="device-list-item ${richControls ? 'has-rich-controls' : ''}">
    <div class="device-list-main">
      <span class="device-glyph ${cls}">${icon}</span>
      <div class="device-list-title"><h3 title="${esc(d.name)}">${esc(d.name)}</h3><small>${esc(deviceBrandGroup(d))} <span>·</span> ${esc(d.room || '未分区')}</small></div>
      ${status}
      ${renderDeviceHoverActions(d)}
    </div>
    ${renderDeviceAttributes(d)}
    ${renderEnergyMetrics(d.energy)}
    <div class="device-list-actions"><span class="device-state">${esc(summary)}</span><div class="device-list-command-area">${renderDeviceControls(d)}</div></div>
  </article>`;
}

const categoryOrder = ['插座','开关','灯光','摄像头','音箱','温湿度计','传感器','空调','扫地机器人','风扇','影音设备','洗碗机','冰箱','加湿器','空气净化器','智能门锁','窗帘','穿戴设备','网络设备','其他'];
function syncDeviceFilters(devices) {
  const brandSelect = $('#brand-filter');
  const categorySelect = $('#category-filter');
  if (!brandSelect || !categorySelect) return;
  const oldBrand = brandSelect.value, oldCategory = categorySelect.value;
  const brands = [...new Set(devices.map(deviceBrandGroup).filter(Boolean))].sort((a,b) => (a.startsWith('小米')?-1:b.startsWith('小米')?1:a.startsWith('易微联')?-1:b.startsWith('易微联')?1:a.localeCompare(b,'zh-CN')));
  const categories = [...new Set(devices.map(d => d.category || '其他'))].sort((a,b) => {
    const ai=categoryOrder.indexOf(a), bi=categoryOrder.indexOf(b); return (ai<0?99:ai)-(bi<0?99:bi) || a.localeCompare(b,'zh-CN');
  });
  brandSelect.innerHTML = '<option value="">所有品牌</option>' + brands.map(v => `<option value="${esc(v)}">${esc(v)}</option>`).join('');
  categorySelect.innerHTML = '<option value="">所有类型</option>' + categories.map(v => `<option value="${esc(v)}">${esc(v)}</option>`).join('');
  brandSelect.value = brands.includes(oldBrand) ? oldBrand : '';
  categorySelect.value = categories.includes(oldCategory) ? oldCategory : '';
}
function renderGroupedDevices(devices) {
  const byBrand = new Map();
  for (const device of devices) {
    const brand = deviceBrandGroup(device);
    if (!byBrand.has(brand)) byBrand.set(brand, []);
    byBrand.get(brand).push(device);
  }
  const brands = [...byBrand.keys()].sort((a,b) => (a.startsWith('小米')?-1:b.startsWith('小米')?1:a.startsWith('易微联')?-1:b.startsWith('易微联')?1:a.localeCompare(b,'zh-CN')));
  return brands.map(brand => {
    const brandDevices = byBrand.get(brand);
    const byCategory = new Map();
    for (const device of brandDevices) {
      const category = device.category || '其他';
      if (!byCategory.has(category)) byCategory.set(category, []);
      byCategory.get(category).push(device);
    }
    const categories = [...byCategory.keys()].sort((a,b) => {
      const ai=categoryOrder.indexOf(a), bi=categoryOrder.indexOf(b); return (ai<0?99:ai)-(bi<0?99:bi) || a.localeCompare(b,'zh-CN');
    });
    return `<section class="device-brand-group"><header class="device-brand-heading"><h2>${esc(brand)}</h2><span class="brand-device-count">${brandDevices.length} 台设备</span></header>${categories.map(category => {
      const items = byCategory.get(category);
      return `<section class="device-category-group"><header class="device-category-heading"><h3>${esc(category)}</h3><span>${items.length} 台</span></header><div class="device-list">${items.map(renderDeviceRow).join('<hr class="device-separator">')}</div></section>`;
    }).join('')}</section>`;
  }).join('');
}
function renderDevices() {
  state.devices = asArray(state.devices);
  syncDeviceFilters(state.devices);
  const recent = state.devices.slice().sort((a,b) => new Date(b.lastSeen || b.createdAt || 0) - new Date(a.lastSeen || a.createdAt || 0)).slice(0,4);
  $('#overview-devices').innerHTML = recent.length ? recent.map(renderDeviceCard).join('') : '<div class="empty-state">还没有设备，点击添加设备开始使用。</div>';
  const q = ($('#device-search')?.value || '').trim().toLowerCase(); const brand = $('#brand-filter')?.value || ''; const category = $('#category-filter')?.value || '';
  const filtered = state.devices.filter(d => (!q || [d.name,d.brand,d.room,d.model,d.category].join(' ').toLowerCase().includes(q)) && (!brand || deviceBrandGroup(d)===brand) && (!category || d.category===category));
  $('#all-devices').innerHTML = filtered.length ? renderGroupedDevices(filtered) : '<div class="empty-state">没有匹配的设备。</div>';
  $('#device-count-label').textContent = `${filtered.length} 台设备 · ${new Set(filtered.map(deviceBrandGroup)).size} 个品牌`;
  $('#nav-device-count').textContent = String(state.devices.length).padStart(2,'0');
}
function renderActivity(target, logs, compact=false) {
  const list = asArray(logs).slice(0, compact ? 4 : 100);
  if (!list.length) { $(target).innerHTML = '<div class="empty-state">暂无运行记录。</div>'; return; }
  $(target).innerHTML = list.map(log => {
    const level = log.level || 'info'; const icon = level==='success' ? '✓' : level==='warn' ? '!' : '·';
    return `<div class="activity-item"><div class="activity-icon ${level==='success'?'success':level==='warn'?'warn':''}">${icon}</div><div class="activity-copy"><strong>${esc(log.message)}</strong><small>GoHome · 本地服务</small></div><span class="activity-time">${timeAgo(log.at)}</span></div>`;
  }).join('');
}
function renderLogs() {
  state.logs = asArray(state.logs);
  if (!state.logs.length) { $('#log-list').innerHTML='<div class="empty-state">暂无日志。</div>'; return; }
  $('#log-list').innerHTML=state.logs.map(l=>`<div class="log-row"><span class="level-tag ${l.level==='error'?'error':l.level==='warn'?'warn':l.level==='info'?'info':''}">${esc(l.level.toUpperCase())}</span><span class="log-message" title="${esc(l.message)}">${esc(l.message)}</span><time class="log-time">${fmtTime(l.at)}</time><button class="log-copy" type="button" data-copy-log="${esc(l.id)}" aria-label="复制日志">复制</button></div>`).join('');
}
function renderAccounts() {
  state.accounts = asArray(state.accounts);
  if (!state.accounts.length) { $('#account-list').innerHTML='<div class="empty-state">尚未连接品牌账号。点击上方「登录」按钮开始授权。</div>'; return; }
  const statusText = { connected:'已连接', sync_error:'同步异常', not_connected:'未授权', verification_required:'需要验证' };
  $('#account-list').innerHTML=state.accounts.map(a=>{const displayName=a.displayName||a.label||'品牌账号';const original=a.displayName&&a.label&&a.displayName!==a.label?`账号：${esc(a.label)} · `:'';return `<div class="account-row"><div class="account-brand">${a.provider==='xiaomi'?'米':'S'}</div><div class="account-info"><strong>${esc(displayName)}</strong><small>${original}${a.provider==='xiaomi'?'Xiaomi / 米家':'eWeLink / Sonoff'} · ${esc(a.region||'默认区域')} · ${esc(a.note||'')}</small></div><span class="account-state ${esc(a.status)}">${esc(statusText[a.status]||a.status||'未知')}</span><div class="account-buttons"><button class="btn btn-small btn-outline" data-sync-account="${esc(a.id)}">同步</button><button class="rename-link" data-rename-account="${esc(a.id)}">重命名</button><button class="delete-link" data-delete-account="${esc(a.id)}">删除</button></div></div>`}).join('');
}
function renderDiscovery() {
  state.discovery = asArray(state.discovery);
  const items=state.discovery; $('#discovery-count').textContent=`${items.length} 项服务`;
  if (!items.length) { $('#discovery-list').innerHTML='<div class="empty-state">暂无发现结果。点击「开始扫描」搜索局域网广播服务。</div>'; return; }
  $('#discovery-list').innerHTML=items.map(d=>`<div class="discovery-row"><div class="discovery-glyph">⌁</div><div class="discovery-copy"><strong>${esc(d.name||d.service)}</strong><small>${esc(d.service)} · ${esc(d.host||'主机名未知')} · 网卡 ${esc(d.interface||'未知')}</small></div><span class="discovery-port">${esc(d.address||'未解析 IP')}${d.port?':'+d.port:''}</span></div>`).join('');
}
function showPage(name) {
  state.page=name;
  if (shouldSyncProviderStates()) startStateSyncActivity(); else stopStateSyncActivity();
  $$('.page').forEach(p=>p.classList.toggle('active',p.id===`page-${name}`));
  $$('.nav-item').forEach(b=>b.classList.toggle('active',b.dataset.page===name));
  const titles={overview:'总览',devices:'设备管理',integrations:'品牌接入',discovery:'局域网发现',logs:'运行日志'};
  $('#crumb-title').textContent=titles[name]||'总览'; $('#sidebar').classList.remove('open'); window.scrollTo({top:0,behavior:'smooth'});
  if(name==='integrations') loadAccounts(); if(name==='discovery') loadDiscovery(); if(name==='logs') loadLogs();
}
async function loadAll() {
  try {
    const [overview, devices, logs, accounts, discovery] = await Promise.all([
      api('/api/overview'), api('/api/devices'), api('/api/logs'), api('/api/accounts'), api('/api/discovery')
    ]);
    state.devices=asArray(devices); state.logs=asArray(logs); state.accounts=asArray(accounts); state.discovery=asArray(discovery);
    state.liveRevision = 0; // force one small live snapshot after a full page load.
    $('#stat-total').textContent=overview.deviceCount; $('#stat-online').textContent=overview.onlineCount;
    $('#stat-on').textContent=overview.onCount; $('#stat-accounts').textContent=overview.accountCount;
    $('#online-rate').textContent=overview.deviceCount?`${Math.round(overview.onlineCount/overview.deviceCount*100)}% 在线率`:'暂无设备';
    renderDevices(); renderActivity('#overview-logs',state.logs,true); renderAccounts(); renderDiscovery(); renderLogs();
  } catch(e) { toast(e.message,'error'); }
}
function updateOverviewLiveCounters() {
  const total = state.devices.length;
  const online = state.devices.filter(d => d.online).length;
  const on = state.devices.reduce((sum, d) => {
    const channels = asArray(d.channels);
    if (channels.length) return sum + channels.filter(ch => ch.power === true).length;
    return sum + (d.power === true ? 1 : 0);
  }, 0);
  if ($('#stat-total')) $('#stat-total').textContent = String(total);
  if ($('#stat-online')) $('#stat-online').textContent = String(online);
  if ($('#stat-on')) $('#stat-on').textContent = String(on);
  if ($('#online-rate')) $('#online-rate').textContent = total ? `${Math.round(online / total * 100)}% 在线率` : '暂无设备';
}
function updateLocalCommandPower(device, enabled, channel) {
  if (!device) return;
  const channels = asArray(device.channels);
  if (channel !== undefined && channel !== null && channels.length) {
    const item = channels.find(ch => Number(ch.outlet) === Number(channel));
    if (item) item.power = enabled;
    // A single-channel relay is still represented by the device-level switch
    // in the compact card. Mirror its only channel into Device.Power as well.
    if (channels.length === 1) device.power = enabled;
    else if (Number(channel) === 0) device.power = enabled;
  } else {
    device.power = enabled;
  }
  if (device.power === enabled && (channel === undefined || channels.length <= 1 || Number(channel) === 0)) {
    const attributes = asArray(device.attributes);
    let relay = attributes.find(a => a.key === 'on' || a.label === '开关状态' || a.label === '电源开关');
    if (!relay) {
      relay = { key: 'on', label: '开关状态', value: enabled ? '开启' : '关闭' };
      attributes.push(relay);
    } else {
      relay.key = 'on'; relay.label = '开关状态'; relay.value = enabled ? '开启' : '关闭';
    }
    device.attributes = attributes;
    if (String(device.model || '').trim().toLowerCase() === 'chuangmi.plug.m3') {
      const led = attributes.find(a => a.key === 'indicator_light');
      if (led) led.label = '指示灯';
    }
  }
  device.online = true;
  device.lastSeen = new Date().toISOString();
}
function applyCommandDeviceSnapshot(snapshot) {
  if (!snapshot || !snapshot.id) return;
  const current = state.devices.find(d => d.id === snapshot.id);
  if (current) Object.assign(current, snapshot);
  else state.devices.push(snapshot);
  renderDevices();
  updateOverviewLiveCounters();
}

async function refreshLiveAfterControl() {
  // A successful command updates the server-side live revision. Read only that
  // delta; do not reload accounts, discovery, logs or rebuild the whole page.
  if (shouldSyncProviderStates()) await pollLiveDeviceStates(true);
  else await loadAll();
}
async function pollLiveDeviceStates(force = false) {
  if (liveDeviceBusy || !shouldSyncProviderStates()) return;
  liveDeviceBusy = true;
  try {
    const since = force ? 0 : state.liveRevision;
    const result = await api(`/api/devices/live?since=${encodeURIComponent(since)}`);
    if (!Array.isArray(result.devices)) return; // 204 = no change; keep the DOM untouched.
    const byID = new Map(state.devices.map(device => [device.id, device]));
    let changed = false;
    for (const live of result.devices) {
      const current = byID.get(live.id);
      if (!current) continue;
      const next = {
        online: live.online,
        power: live.power ?? null,
        channels: asArray(live.channels),
        energy: live.energy ?? null,
        attributes: asArray(live.attributes),
        value: live.value || '',
        lastSeen: live.lastSeen || current.lastSeen,
      };
      const controlValues = asArray(live.controlValues);
      for (const item of controlValues) {
        const control = asArray(current.controls).find(candidate => candidate.id === item.id);
        if (control && JSON.stringify(control.value) !== JSON.stringify(item.value)) {
          control.value = item.value;
          changed = true;
        }
      }
      const oldFingerprint = JSON.stringify([current.online, current.power ?? null, current.channels || [], current.energy || null, current.attributes || [], current.value || '']);
      const newFingerprint = JSON.stringify([next.online, next.power, next.channels, next.energy, next.attributes, next.value]);
      if (oldFingerprint !== newFingerprint) changed = true;
      Object.assign(current, next);
    }
    state.liveRevision = Number(result.revision || state.liveRevision || 0);
    if (changed) {
      renderDevices();
      updateOverviewLiveCounters();
    }
  } catch (error) {
    // Keep the page quiet on a transient poll error; normal API actions still
    // surface their own errors. A stale revision will be retried next tick.
  } finally {
    liveDeviceBusy = false;
  }
}
async function loadAccounts(){ try{state.accounts=asArray(await api('/api/accounts'));renderAccounts()}catch(e){toast(e.message,'error')} }
async function loadDiscovery(){ try{state.discovery=asArray(await api('/api/discovery'));renderDiscovery()}catch(e){toast(e.message,'error')} }
async function loadLogs(){ try{state.logs=asArray(await api('/api/logs'));renderActivity('#overview-logs',state.logs,true);renderLogs()}catch(e){toast(e.message,'error')} }

function openModal(id){$(id).classList.add('show');$(id).setAttribute('aria-hidden','false')}
function closeModal(id){$(id).classList.remove('show');$(id).setAttribute('aria-hidden','true')}
function askAccount(provider){
  const form=$('#account-form'); form.reset(); $('#account-error-panel').hidden=true; state.xiaomiChallengeId=''; state.xiaomiChallengeType='';
  $('#account-provider').value=provider;
  $('#account-modal-title').textContent=provider==='xiaomi'?'登录 Xiaomi / 米家':'登录 eWeLink / Sonoff';
  $('#xiaomi-fields').hidden=provider!=='xiaomi'; $('#ewelink-fields').hidden=provider!=='ewelink';
  $('#xiaomi-captcha').hidden=true; $('#xiaomi-verify-panel').hidden=true; $('#xiaomi-verify-code').hidden=false; $('#xiaomi-resend').textContent='重新发送验证码'; $('#account-submit').textContent='登录并同步设备';
  openModal('#account-backdrop');
}

async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const area = document.createElement('textarea');
  area.value = text;
  area.style.position = 'fixed'; area.style.opacity = '0'; area.style.pointerEvents = 'none';
  document.body.appendChild(area); area.select();
  const ok = document.execCommand('copy'); area.remove();
  if (!ok) throw new Error('浏览器不允许复制，请检查剪贴板权限');
}

function parsePromptValue(text, input) {
  const format = String(input.format || '').toLowerCase();
  if (input.options?.length) {
    const found = input.options.find(x => String(x.value) === text || x.label === text);
    if (found) return found.value;
    try { return JSON.parse(text); } catch { throw new Error('输入值不在规格选项中'); }
  }
  if (format === 'bool' || format === 'boolean') {
    if (text.toLowerCase() === 'true') return true;
    if (text.toLowerCase() === 'false') return false;
    throw new Error('请输入 true 或 false');
  }
  if (/^(u?int|float|double)/.test(format)) {
    const number = Number(text);
    if (!Number.isFinite(number)) throw new Error('请输入有效数字');
    return number;
  }
  return text;
}

// Navigation and shared actions
document.addEventListener('click', async (e) => {
  const nav=e.target.closest('[data-page]'); if(nav){showPage(nav.dataset.page);return;}
  const goto=e.target.closest('[data-goto]'); if(goto){showPage(goto.dataset.goto);return;}
  const add=e.target.closest('[data-action]'); if(add){
    if(add.dataset.action==='add-device') openModal('#modal-backdrop');
    if(add.dataset.action==='add-xiaomi') askAccount('xiaomi');
    if(add.dataset.action==='add-ewelink') askAccount('ewelink');
    return;
  }
  const cmd=e.target.closest('[data-command]'); if(cmd){
    const device=state.devices.find(d=>d.id===cmd.dataset.id);
    const channel=cmd.dataset.channel===undefined?undefined:Number(cmd.dataset.channel);
    const pendingKey=`${cmd.dataset.id}:${channel===undefined?'default':channel}`;
    if(pendingDeviceCommands.has(pendingKey))return;
    pendingDeviceCommands.add(pendingKey);
    cmd.disabled=true;
    const channelState=channel===undefined?device?.power:asArray(device?.channels).find(ch=>Number(ch.outlet)===channel)?.power;
    const desired=cmd.dataset.command==='turn_on'?true:cmd.dataset.command==='turn_off'?false:!Boolean(channelState);
    const previous=device?{power:device.power,channels:asArray(device.channels).map(ch=>({...ch})),attributes:asArray(device.attributes).map(a=>({...a})),online:device.online,lastSeen:device.lastSeen}:null;
    if(device){updateLocalCommandPower(device,desired,channel);renderDevices();updateOverviewLiveCounters();}
    try{
      const body={command:cmd.dataset.command};if(channel!==undefined)body.channel=channel;
      const result=await api(`/api/devices/${encodeURIComponent(cmd.dataset.id)}/command`,{method:'POST',body:JSON.stringify(body)});
      applyCommandDeviceSnapshot(result);
      // The next live delta reconciles any provider-side state correction.
      void refreshLiveAfterControl();
      toast('控制命令已发送','success');
    }catch(err){
      if(device&&previous){Object.assign(device,previous);renderDevices();updateOverviewLiveCounters();}
      toast(err.message,'error');
    }finally{
      pendingDeviceCommands.delete(pendingKey);
      renderDevices();
      cmd.disabled=false;
    }
    return;
  }
  const copyAPI=e.target.closest('[data-copy-device-api]'); if(copyAPI){const device=state.devices.find(d=>d.id===copyAPI.dataset.copyDeviceApi);if(!device){toast('设备已不存在，请刷新列表','error');return;}try{await copyText(buildDeviceAPIText(device));toast('设备 API 请求示例已复制','success')}catch(err){toast(err.message,'error')}return;}
  const deleteDevice=e.target.closest('[data-delete-device]'); if(deleteDevice){const device=state.devices.find(d=>d.id===deleteDevice.dataset.deleteDevice);if(!device){toast('设备已不存在，请刷新列表','error');return;}const ok=confirm(`确定永久删除“${device.name}”？普通设备同步不会重新添加；删除品牌账号并重新登录后，可以重新同步恢复。`);if(!ok)return;deleteDevice.disabled=true;try{await api(`/api/devices/${encodeURIComponent(device.id)}`,{method:'DELETE'});state.devices=state.devices.filter(d=>d.id!==device.id);renderDevices();toast('设备已删除；重新登录品牌账号后可恢复','success')}catch(err){toast(err.message,'error')}finally{deleteDevice.disabled=false}return;}
  const prop=e.target.closest('[data-miot-property]'); if(prop){prop.disabled=true;try{const value=prop.dataset.value==='true';await api(`/api/v1/devices/${encodeURIComponent(prop.dataset.id)}/property`,{method:'POST',body:JSON.stringify({siid:Number(prop.dataset.siid),piid:Number(prop.dataset.piid),value})});await refreshLiveAfterControl();toast('MIoT 属性写入成功','success')}catch(err){toast(err.message,'error')}finally{prop.disabled=false}return;}
  const setProp=e.target.closest('[data-miot-set-property]'); if(setProp){setProp.disabled=true;try{const field=document.getElementById(setProp.dataset.inputId);if(!field)throw new Error('找不到属性输入框，请刷新设备列表');const format=String(setProp.dataset.format||'').toLowerCase();if(field.value.trim()==='')throw new Error('请先输入属性值');let value=field.value;if(/^(u?int|float|double)/.test(format)){value=Number(field.value);if(!Number.isFinite(value))throw new Error('请输入有效数字');}await api(`/api/v1/devices/${encodeURIComponent(setProp.dataset.id)}/property`,{method:'POST',body:JSON.stringify({siid:Number(setProp.dataset.siid),piid:Number(setProp.dataset.piid),value})});await refreshLiveAfterControl();toast('MIoT 属性写入成功','success')}catch(err){toast(err.message,'error')}finally{setProp.disabled=false}return;}
  const action=e.target.closest('[data-miot-action]'); if(action){action.disabled=true;try{const device=state.devices.find(d=>d.id===action.dataset.id);const control=asArray(device?.controls).find(c=>c.id===action.dataset.control);if(!control)throw new Error('动作能力已变化，请重新同步设备');const params=[];for(let i=0;i<asArray(control.inputs).length;i++){const input=control.inputs[i];const hint=input.options?.length?`可选：${input.options.map(x=>`${x.label}=${JSON.stringify(x.value)}`).join('、')}`:`格式：${input.format||'文本'}`;const initial=input.options?.length?String(input.options[0].value):['bool','boolean'].includes(String(input.format || '').toLowerCase())?'false':/^(u?int|float|double)/i.test(input.format||'')?'0':'';const entered=prompt(`${input.name}\n${hint}`,initial);if(entered===null)throw new Error('已取消操作');params.push(parsePromptValue(entered,input));}await api(`/api/v1/devices/${encodeURIComponent(action.dataset.id)}/action`,{method:'POST',body:JSON.stringify({siid:Number(action.dataset.siid),aiid:Number(action.dataset.aiid),params})});await refreshLiveAfterControl();toast(`已执行：${control.name}`,'success')}catch(err){if(err.message!=='已取消操作')toast(err.message,'error')}finally{action.disabled=false}return;}
  const rename=e.target.closest('[data-rename-account]'); if(rename){const account=state.accounts.find(a=>a.id===rename.dataset.renameAccount);if(!account){toast('品牌账号不存在，请刷新列表','error');return;}const current=account.displayName||account.label||'';const entered=prompt('设置这个品牌账号在 GoHome 中的显示名称：\n不会修改登录账号、Token 或授权状态。',current);if(entered===null)return;const displayName=entered.trim();if(Array.from(displayName).length>48){toast('显示名称不能超过 48 个字符','error');return;}rename.disabled=true;try{const result=await api(`/api/accounts/${encodeURIComponent(account.id)}`,{method:'PATCH',body:JSON.stringify({displayName})});const updated=result.account||{...account,displayName};state.accounts=state.accounts.map(item=>item.id===account.id?updated:item);renderAccounts();toast(displayName?'品牌账号显示名称已更新':'已恢复原始账号名称','success')}catch(err){toast(err.message,'error')}finally{rename.disabled=false}return;}
  const sync=e.target.closest('[data-sync-account]'); if(sync){sync.disabled=true;sync.textContent='同步中';try{const result=await api(`/api/accounts/${encodeURIComponent(sync.dataset.syncAccount)}/sync`,{method:'POST',body:'{}'});await loadAll();toast(`设备同步完成：${result.deviceCount} 台`,'success')}catch(err){toast(err.message,'error')}finally{sync.disabled=false;sync.textContent='同步'}return;}
  const del=e.target.closest('[data-delete-account]'); if(del){if(!confirm('确定删除该品牌账号及其加密凭证、已导入设备吗？'))return;try{await api(`/api/accounts/${encodeURIComponent(del.dataset.deleteAccount)}`,{method:'DELETE'});await loadAll();toast('品牌账号及关联设备已删除','success')}catch(err){toast(err.message,'error')}return;}
});

document.addEventListener('change', async e => {
  const select=e.target.closest('.miot-control-select');
  if(!select)return;
  select.disabled=true;
  try{
    const value=JSON.parse(select.value);
    await api(`/api/v1/devices/${encodeURIComponent(select.dataset.id)}/property`,{method:'POST',body:JSON.stringify({siid:Number(select.dataset.siid),piid:Number(select.dataset.piid),value})});
    await refreshLiveAfterControl(); toast('设备属性已更新','success');
  }catch(err){toast(err.message,'error');}
  finally{select.disabled=false;}
});

$('#device-form').addEventListener('submit',async e=>{
  e.preventDefault();
  // currentTarget is only guaranteed during the synchronous event dispatch.
  // Keep a stable reference before the first await.
  const form=e.currentTarget;
  const submitButton=form.querySelector('button[type="submit"]');
  const data=Object.fromEntries(new FormData(form).entries());
  if(submitButton) submitButton.disabled=true;
  try{
    await api('/api/devices',{method:'POST',body:JSON.stringify(data)});
    form.reset();
    closeModal('#modal-backdrop');
    await loadAll();
    showPage('devices');
    toast('设备档案已创建；真实控制驱动尚未接入','success');
  }catch(err){toast(err.message,'error')}
  finally{if(submitButton) submitButton.disabled=false;}
});
function updateXiaomiVerifyChannels(result={}) {
  const row=$('#xiaomi-verify-channel-row'), select=$('#xiaomi-verify-channel');
  const channels=Array.isArray(result.channels)?result.channels:[];
  select.replaceChildren();
  for(const item of channels){
    const option=document.createElement('option'); option.value=String(item.flag); option.textContent=item.label||item.channel||String(item.flag);
    select.appendChild(option);
  }
  const selected=Number(result.selectedFlag||state.xiaomiVerifyFlag||channels[0]?.flag||0);
  state.xiaomiVerifyFlag=selected;
  if(selected)select.value=String(selected);
  row.hidden=channels.length<2;
  select.onchange=()=>{state.xiaomiVerifyFlag=Number(select.value)||0;};
}

$('#account-form').addEventListener('submit',async e=>{
  e.preventDefault();
  const form=e.currentTarget;
  const submitButton=$('#account-submit');
  const provider=$('#account-provider').value;
  const value=name=>form.querySelector(`[name="${name}"]`)?.value?.trim()||'';
  $('#account-error-panel').hidden=true;
  submitButton.disabled=true;
  try{
    let result;
    if(provider==='xiaomi'){
      if(state.xiaomiChallengeType==='verify'){
        result=await api('/api/integrations/xiaomi/verify',{method:'POST',body:JSON.stringify({challengeId:state.xiaomiChallengeId,ticket:$('#xiaomi-verify-code').value.trim()})});
      }else if(state.xiaomiChallengeType==='sendRetry'){
        result=await api('/api/integrations/xiaomi/verify/send',{method:'POST',body:JSON.stringify({challengeId:state.xiaomiChallengeId,resend:false,flag:state.xiaomiVerifyFlag})});
      }else if(state.xiaomiChallengeType==='identityCaptcha'){
        result=await api('/api/integrations/xiaomi/verify/send',{method:'POST',body:JSON.stringify({challengeId:state.xiaomiChallengeId,captcha:$('#xiaomi-captcha-code').value.trim(),flag:state.xiaomiVerifyFlag})});
      }else if(state.xiaomiChallengeType==='captcha'){
        result=await api('/api/integrations/xiaomi/captcha',{method:'POST',body:JSON.stringify({challengeId:state.xiaomiChallengeId,code:$('#xiaomi-captcha-code').value.trim()})});
      }else{
        result=await api('/api/integrations/xiaomi/login',{method:'POST',body:JSON.stringify({label:value('label'),username:value('username'),password:value('password'),region:value('region')||'auto'})});
      }
      if(result.challenge){
        state.xiaomiChallengeId=result.challengeId;state.xiaomiChallengeType='captcha';
        $('#xiaomi-captcha-image').src=result.captchaImage;$('#xiaomi-captcha').hidden=false;$('#xiaomi-verify-panel').hidden=true;
        $('#xiaomi-captcha-code').value='';$('#account-submit').textContent='提交图形验证码';
        toast(result.message||'请输入小米图形验证码');return;
      }
      if(result.identityCaptcha){
        state.xiaomiChallengeId=result.challengeId;state.xiaomiChallengeType='identityCaptcha';
        $('#xiaomi-captcha-image').src=result.captchaImage;$('#xiaomi-captcha').hidden=false;$('#xiaomi-verify-panel').hidden=true;
        $('#xiaomi-captcha-code').value='';$('#account-submit').textContent='提交验证码并发送短信';
        toast(result.message||'请完成小米图形验证');return;
      }
      if(result.verifyPending){
        state.xiaomiChallengeId=result.challengeId;state.xiaomiChallengeType='sendRetry';updateXiaomiVerifyChannels(result);
        $('#xiaomi-captcha').hidden=true;$('#xiaomi-verify-panel').hidden=false;
        $('#xiaomi-verify-code').hidden=true;$('#xiaomi-resend').textContent=result.errorCode===70022?'切换验证方式 / 重新发送':'重试读取验证方式';
        $('#xiaomi-verify-hint').textContent=result.message||'小米已进入安全验证，但暂时无法读取验证方式。请在当前会话中重试，不要重新登录。';
        $('#account-submit').textContent='重试验证流程';
        toast('已保留本次小米验证会话，请勿重新登录','warning');return;
      }
      if(result.verifyRequired){
        state.xiaomiChallengeId=result.challengeId;state.xiaomiChallengeType='verify';updateXiaomiVerifyChannels(result);
        $('#xiaomi-captcha').hidden=true;$('#xiaomi-verify-panel').hidden=false;
        $('#xiaomi-verify-hint').textContent=result.message||`小米验证码已发送（${result.channel==='Phone'?'短信':'邮箱'}），请输入验证码。`;
        $('#xiaomi-verify-code').hidden=false;$('#xiaomi-resend').textContent='重新发送验证码';
        $('#xiaomi-verify-code').value='';$('#account-submit').textContent='验证并登录';
        toast(result.message||'请输入收到的小米验证码');return;
      }
    }else if(provider==='ewelink'){
      result=await api('/api/integrations/ewelink/login',{method:'POST',body:JSON.stringify({
        label:value('label'),username:value('ewelinkUsername'),countryCode:value('countryCode')||'+86',region:value('region')||'auto',password:value('ewelinkPassword')
      })});
    }else{throw new Error('未知品牌接入类型')}
    closeModal('#account-backdrop');form.reset();$('#account-error-panel').hidden=true;state.xiaomiChallengeId='';state.xiaomiChallengeType='';$('#xiaomi-captcha').hidden=true;$('#xiaomi-verify-panel').hidden=true;
    await loadAll();showPage('integrations');
    if(result.warning) toast(result.warning,'error');
    else toast(`登录成功，已同步 ${result.deviceCount||0} 台设备`,'success');
  }catch(err){
    $('#account-error-message').textContent=err.message||'未知错误';
    $('#account-error-panel').hidden=false;
    await loadLogs();
    toast(err.message,'error');
  }
  finally{
    // Keep credentials after a failed login so the user can correct only the
    // relevant field. A successful login calls form.reset() above.
    submitButton.disabled=false;
  }
});
$('#xiaomi-resend').addEventListener('click',async()=>{
  const btn=$('#xiaomi-resend');btn.disabled=true;
  try{const result=await api('/api/integrations/xiaomi/verify/send',{method:'POST',body:JSON.stringify({challengeId:state.xiaomiChallengeId,resend:state.xiaomiChallengeType==='verify',flag:state.xiaomiVerifyFlag})});
    if(result.identityCaptcha){state.xiaomiChallengeType='identityCaptcha';$('#xiaomi-verify-panel').hidden=true;$('#xiaomi-captcha').hidden=false;$('#xiaomi-captcha-image').src=result.captchaImage;$('#account-submit').textContent='提交验证码并发送短信';}
    else if(result.verifyPending){state.xiaomiChallengeType='sendRetry';updateXiaomiVerifyChannels(result);$('#xiaomi-verify-panel').hidden=false;$('#xiaomi-verify-code').hidden=true;$('#xiaomi-verify-hint').textContent=result.message||'小米暂时拒绝发送验证码，请更换验证方式或稍后重试。';toast('验证会话已保留','warning');}
    else if(result.verifyRequired){state.xiaomiChallengeType='verify';updateXiaomiVerifyChannels(result);$('#xiaomi-verify-panel').hidden=false;$('#xiaomi-verify-code').hidden=false;$('#xiaomi-resend').textContent='重新发送验证码';$('#account-submit').textContent='验证并登录';$('#xiaomi-verify-hint').textContent=result.message||'验证码已发送，请查收。';toast(result.message||'验证码已发送','success');}
    else toast(result.message||'已更新小米验证状态','success');
  }catch(err){$('#account-error-message').textContent=err.message||'未知错误';$('#account-error-panel').hidden=false;$('#xiaomi-verify-hint').textContent=err.message||'验证码发送失败；当前验证会话可能仍然有效。';await loadLogs();toast(err.message,'error')}
  finally{btn.disabled=false;}
});
$('#modal-close').onclick=()=>closeModal('#modal-backdrop');$('#modal-cancel').onclick=()=>closeModal('#modal-backdrop');
$('#account-close').onclick=()=>{state.xiaomiChallengeId='';state.xiaomiChallengeType='';closeModal('#account-backdrop')};$('#account-cancel').onclick=()=>{state.xiaomiChallengeId='';state.xiaomiChallengeType='';closeModal('#account-backdrop')};
$$('.modal-backdrop').forEach(el=>el.addEventListener('click',e=>{if(e.target===el)closeModal('#'+el.id)}));
async function copyText(text){
  try{
    if(navigator.clipboard && window.isSecureContext){await navigator.clipboard.writeText(text);return true;}
    const box=document.createElement('textarea');box.value=text;box.style.position='fixed';box.style.opacity='0';document.body.appendChild(box);box.select();const ok=document.execCommand('copy');box.remove();return ok;
  }catch{return false;}
}
$('#account-error-copy').addEventListener('click',async()=>{
  const text=$('#account-error-message').textContent||'';
  const ok=await copyText(text);toast(ok?'错误内容已复制':'复制失败，请手动选择错误文本',ok?'success':'error');
});
document.addEventListener('click',async e=>{
  const button=e.target.closest('[data-copy-log]');
  if(!button)return;
  const item=state.logs.find(x=>x.id===button.dataset.copyLog);
  if(!item)return;
  const text=`${fmtTime(item.at)} [${String(item.level||'info').toUpperCase()}] ${item.message}`;
  const ok=await copyText(text);toast(ok?'日志已复制，可以粘贴发送':'复制失败，请选择日志文本后手动复制',ok?'success':'error');
});
$('#logs-copy').addEventListener('click',async()=>{
  const text=state.logs.map(item=>`${fmtTime(item.at)} [${String(item.level||'info').toUpperCase()}] ${item.message}`).join('\n');
  if(!text){toast('暂无日志','warning');return;}
  const ok=await copyText(text);toast(ok?'全部日志已复制':'复制失败，请手动选择日志',ok?'success':'error');
});
$('#device-search').addEventListener('input',renderDevices);$('#brand-filter').addEventListener('change',renderDevices);$('#category-filter').addEventListener('change',renderDevices);
$('#refresh-btn').onclick=async()=>{await loadAll();toast('数据已刷新','success')};$('#logs-refresh').onclick=loadLogs;
$('#mobile-menu').onclick=()=>$('#sidebar').classList.toggle('open');
$('#scan-btn').onclick=async()=>{
  const btn=$('#scan-btn');btn.disabled=true;btn.textContent='扫描中…';
  try{const result=await api('/api/discovery/scan',{method:'POST',body:'{}'});state.discovery=asArray(result.items);renderDiscovery();await loadLogs();toast(`扫描完成，发现 ${result.count} 项服务`,'success')}
  catch(err){toast(err.message,'error')}
  finally{btn.disabled=false;btn.textContent='⌁ 开始扫描'}
};

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  const form=event.currentTarget;
  const button=form.querySelector('button[type="submit"]');
  const username=form.elements.username.value.trim();
  const password=form.elements.password.value;
  button.disabled=true; $('#login-error').hidden=true;
  try {
    await api('/api/auth/login',{method:'POST',body:JSON.stringify({username,password})});
    form.elements.password.value='';
    enterApplication();
    await loadAll(); await loadBuildMetadata();
  } catch(error) {
    $('#login-error').textContent=error.message||'登录失败';
    $('#login-error').hidden=false;
    form.elements.password.focus();
  } finally { button.disabled=false; }
});
$('#logout-btn').addEventListener('click',async()=>{
  try{await api('/api/auth/logout',{method:'POST',body:'{}'});}catch{}
  showLoginScreen('已退出登录');
});
async function bootstrapAuth(){
  try{
    const response=await fetch('/api/auth/session',{credentials:'same-origin'});
    const session=await response.json();
    if(session.authenticated){enterApplication();await loadAll();await loadBuildMetadata();return;}
  }catch{}
  showLoginScreen();
}
bootstrapAuth();
async function loadBuildMetadata(){
  try {
    const meta = await api('/api/health');
    const node = $('#build-meta');
    if (!node) return;
    node.textContent = `GoHome v${meta.version || 'dev'}`;
    node.title = `构建日期：${meta.buildDate || '未知'}\n版本序号：${meta.commitCount || '0'}\nGit 提交：${meta.gitHash || '无 Git 信息'}\n提交内容：${meta.gitMessage || '未提供'}`;
    node.setAttribute('aria-label', node.title.replaceAll('\n', '；'));
  } catch (_) { /* Keep the static footer when metadata is unavailable. */ }
}
// Device state is updated in-place with a small AJAX delta while overview/devices is visible.
// No whole-page reload and no full accounts/logs/discovery fetch on every tick.
document.addEventListener('visibilitychange', () => {
  if (shouldSyncProviderStates()) {
    startStateSyncActivity();
    pollLiveDeviceStates(true);
  } else {
    stopStateSyncActivity();
  }
});
window.addEventListener('pagehide', () => stopStateSyncActivity());

