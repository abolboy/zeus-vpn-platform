import { useEffect, useMemo, useState } from 'react'
import { getControlPlaneHealth } from './api.js'

const initialServers = [
  { id: 1, name: 'ZEUS • Frankfurt', region: 'آلمان', ip: '185.220.101.14', protocol: 'WireGuard', load: 38, latency: 82, status: 'online', users: 128 },
  { id: 2, name: 'ZEUS • Helsinki', region: 'فنلاند', ip: '95.217.12.44', protocol: 'WireGuard', load: 61, latency: 114, status: 'online', users: 86 },
  { id: 3, name: 'ZEUS • Warsaw', region: 'لهستان', ip: '—', protocol: 'در انتظار راه‌اندازی', load: 0, latency: 0, status: 'pending', users: 0 },
]
const initialUsers = [
  { id: 'ZU-2048', name: 'کاربر نمونه ۱', plan: 'نامحدود', usage: '42.8 GB', expiry: '۱۴۰۵/۰۹/۲۸', status: 'active' },
  { id: 'ZU-2049', name: 'کاربر نمونه ۲', plan: '۳۰ روزه', usage: '8.2 GB', expiry: '۱۴۰۵/۰۹/۱۸', status: 'active' },
  { id: 'ZU-2050', name: 'کاربر نمونه ۳', plan: '۷ روزه', usage: '1.1 GB', expiry: '۱۴۰۵/۰۸/۲۹', status: 'expired' },
]
const nav = [
  ['overview', '◫', 'نمای کلی'], ['servers', '⌘', 'سرورها'], ['users', '♙', 'کاربران'],
  ['plans', '◈', 'اشتراک‌ها'], ['activity', '⌁', 'گزارش رویدادها'], ['settings', '⚙', 'تنظیمات'],
]
const themes = [
  { id: 'core', name: 'NEON CORE', sub: 'فیروزه‌ای • بنفش', color: '#55f6df' },
  { id: 'cyber', name: 'CYBERPUNK', sub: 'صورتی • آبی', color: '#ff4fd8' },
  { id: 'aurora', name: 'AURORA', sub: 'سبز • آبی یخی', color: '#a4ff78' },
]

function Metric({ icon, label, value, note, tone = '' }) {
  return <article className="metric glass"><div className="metric-top"><span className="metric-icon">{icon}</span><span className="metric-note">{note}</span></div><div className="metric-value">{value}</div><div className="metric-label">{label}</div><div className={`metric-line ${tone}`} /></article>
}
function Status({ value }) {
  const label = value === 'online' || value === 'active' ? 'فعال' : value === 'pending' ? 'در انتظار' : 'منقضی'
  return <span className={`status ${value}`}><i />{label}</span>
}

export default function App() {
  const [theme, setTheme] = useState('core')
  const [page, setPage] = useState('overview')
  const [servers, setServers] = useState(initialServers)
  const [users, setUsers] = useState(initialUsers)
  const [query, setQuery] = useState('')
  const [notice, setNotice] = useState('')
  const [showServerForm, setShowServerForm] = useState(false)
  const [serverName, setServerName] = useState('')
  const [serverRegion, setServerRegion] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [compact, setCompact] = useState(false)
  const [apiStatus, setApiStatus] = useState('checking')
  const [apiMessage, setApiMessage] = useState('در حال بررسی اتصال کنترل‌پلین')

  async function checkControlPlane() {
    setApiStatus('checking')
    setApiMessage('در حال بررسی اتصال کنترل‌پلین')
    const result = await getControlPlaneHealth()
    setApiStatus(result.state)
    setApiMessage(result.message)
    if (result.state === 'online') toast('ارتباط با Control Plane برقرار است؛ فقط سلامت سرویس تأیید شد.')
    else if (result.state === 'offline') toast('Control Plane پاسخ معتبر نداد.')
    else toast('نشانی API تنظیم نشده است.')
  }

  useEffect(() => {
    let active = true
    getControlPlaneHealth().then(result => {
      if (active) { setApiStatus(result.state); setApiMessage(result.message) }
    })
    return () => { active = false }
  }, [])
  const filteredUsers = useMemo(() => users.filter(u => (u.name + u.id).toLowerCase().includes(query.toLowerCase())), [users, query])
  const activePage = nav.find(n => n[0] === page)?.[2] ?? 'نمای کلی'
  const toast = (message) => { setNotice(message); window.setTimeout(() => setNotice(''), 2600) }

  function addServer(event) {
    event.preventDefault()
    if (!serverName.trim() || !serverRegion.trim()) return
    setServers(current => [...current, { id: Date.now(), name: serverName.trim(), region: serverRegion.trim(), ip: 'پس از اتصال نود', protocol: 'WireGuard', load: 0, latency: 0, status: 'pending', users: 0 }])
    setServerName(''); setServerRegion(''); setShowServerForm(false)
    toast('سرور به فهرست نمایشی اضافه شد؛ هنوز به نود واقعی متصل نیست.')
  }
  function toggleUser(id) {
    setUsers(current => current.map(user => user.id === id ? { ...user, status: user.status === 'active' ? 'expired' : 'active' } : user))
    toast('وضعیت نمایشی کاربر تغییر کرد؛ API هنوز متصل نیست.')
  }

  return <div className={`app theme-${theme} ${compact ? 'compact' : ''}`}>
    <div className="ambient ambient-one" /><div className="ambient ambient-two" />
    <aside className="sidebar glass">
      <div className="brand"><div className="brand-mark">Z</div><div><b>ZEUS</b><small>CONTROL CENTER <span>v0.1</span></small></div></div>
      <div className="workspace"><span className="workspace-dot" /> محیط آزمایشی <span className="chevron">⌄</span></div>
      <div className="nav-caption">مدیریت اصلی</div>
      <nav>{nav.map(([id, icon, label]) => <button key={id} className={page === id ? 'nav-item selected' : 'nav-item'} onClick={() => setPage(id)}><span className="nav-icon">{icon}</span><span>{label}</span>{id === 'activity' && <em>۳</em>}</button>)}</nav>
      <div className="sidebar-bottom"><div className="support-card"><span className="support-orb">✦</span><b>مرکز راهنما</b><p>راهنمای مدیریت ZEUS و وضعیت سرویس‌ها</p><button onClick={() => toast('راهنمای کامل در مرحله مستندسازی قرار دارد.')}>مشاهده راهنما <span>←</span></button></div><div className="profile"><div className="avatar">A</div><div><b>مدیر ZEUS</b><small>دسترسی مدیرکل</small></div><button aria-label="منوی حساب" onClick={() => toast('حساب نمایشی مدیر')}>•••</button></div></div>
    </aside>

    <main className="main">
      <header className="topbar"><div className="crumb">ZEUS <span>/</span> {activePage}</div><div className="top-actions"><span className={`live-indicator api-${apiStatus}`}><i /> {apiStatus === "online" ? "Control Plane متصل" : apiStatus === "offline" ? "Control Plane قطع" : apiStatus === "checking" ? "در حال بررسی API" : "API تنظیم نشده"}</span><button className="icon-btn" aria-label="اعلان‌ها" onClick={() => setPage('activity')}>♧<b className="notification-dot" /></button><button className="language-btn" onClick={() => toast('رابط فارسی فعال است؛ زبان انگلیسی در برنامه توسعه قرار دارد.')}>فا <span>⌄</span></button></div></header>

      <section className="page-head"><div><div className="eyebrow"><span /> SYSTEM OVERVIEW / ۰۱</div><h1>{page === 'overview' ? 'مرکز فرماندهی' : activePage}<span className="heading-glow">.</span></h1><p>{page === 'overview' ? 'نمای زنده از زیرساخت، کاربران و سلامت شبکه ZEUS' : 'مدیریت و پایش بخش ' + activePage + ' در پنل ZEUS'}</p></div><div className="head-actions"><button className="button secondary" onClick={checkControlPlane}>↻ بررسی API</button><button className="button primary" onClick={() => { setPage('servers'); setShowServerForm(true) }}>＋ افزودن سرور</button></div></section>

      <section className="theme-strip glass"><div><b>ظاهر پنل را انتخاب کن</b><small>سه پوسته نئونی با تغییر زنده و بدون بارگذاری مجدد</small></div><div className="theme-options">{themes.map(t => <button key={t.id} onClick={() => setTheme(t.id)} className={theme === t.id ? 'theme-choice active' : 'theme-choice'}><i style={{ background: t.color, boxShadow: `0 0 14px ${t.color}` }} /><span><b>{t.name}</b><small>{t.sub}</small></span>{theme === t.id && <strong>✓</strong>}</button>)}</div></section>

      {page === 'overview' && <><section className="metrics-grid"><Metric icon="⌘" label="سرورهای آنلاین" value="۰۲ / ۰۳" note="+۱ در این هفته" /><Metric icon="♙" label="کاربران فعال" value="۲۱۴" note="نمونه نمایشی" tone="purple" /><Metric icon="⇅" label="ترافیک مصرفی" value="۱.۲۸ TB" note="۳۰ روز اخیر" tone="blue" /><Metric icon="◷" label="میانگین تأخیر" value="۹۸ ms" note="از نودهای نمونه" tone="pink" /></section>
      <section className="content-grid"><article className="panel glass server-panel"><div className="panel-heading"><div><h2>وضعیت سرورها</h2><p>پایش سلامت نودهای ثبت‌شده</p></div><button className="text-link" onClick={() => setPage('servers')}>همه سرورها ←</button></div><div className="server-list">{servers.slice(0,3).map(server => <div className="server-row" key={server.id}><div className="server-symbol">⌘</div><div className="server-info"><b>{server.name}</b><small>{server.region} · {server.protocol}</small></div><div className="server-load"><div className="load-label"><span>بار سرور</span><b>{server.status === 'pending' ? '—' : server.load + '%'}</b></div><div className="progress"><i style={{ width: server.load + '%' }} /></div></div><Status value={server.status} /></div>)}</div></article>
      <article className="panel glass traffic-panel"><div className="panel-heading"><div><h2>ترافیک شبکه</h2><p>نمای نمونه مصرف در ۷ روز اخیر</p></div><span className="select-like">۷ روز اخیر⌄</span></div><div className="traffic-total"><b>۸۴۲.۶ <small>GB</small></b><span>↑ ۱۲.۸٪ <small>نسبت به دوره قبل</small></span></div><div className="chart"><div className="chart-y"><span>۱۰۰GB</span><span>۷۵GB</span><span>۵۰GB</span><span>۲۵GB</span><span>۰</span></div><div className="chart-bars">{[42,68,52,86,61,74,95].map((v,i)=><div className="bar-wrap" key={i}><i style={{height:v+'%'}} /><small>{['ش','ی','د','س','چ','پ','ج'][i]}</small></div>)}</div></div></article></section>
      <section className="panel glass activity-panel"><div className="panel-heading"><div><h2>رویدادهای اخیر</h2><p>آخرین فعالیت‌های ثبت‌شده در داده آزمایشی</p></div><button className="text-link" onClick={() => setPage('activity')}>مشاهده گزارش‌ها ←</button></div><div className="activity-list"><div><i className="activity-icon green">✓</i><span><b>نود Frankfurt در دسترس است</b><small>بررسی نمونه سلامت · ۲ دقیقه پیش</small></span><Status value="online" /></div><div><i className="activity-icon purple">♙</i><span><b>کاربر نمونه به فهرست اضافه شد</b><small>رویداد آزمایشی · ۱۸ دقیقه پیش</small></span><Status value="active" /></div><div><i className="activity-icon orange">!</i><span><b>راه‌اندازی نود Warsaw تکمیل نشده</b><small>نیازمند نصب Agent و تنظیم کلیدها</small></span><Status value="pending" /></div></div></section></>}

      {page === 'servers' && <section className="panel glass full-panel"><div className="panel-heading"><div><h2>مدیریت سرورها</h2><p>فهرست نودها؛ اطلاعات فعلی صرفاً داده نمایشی است</p></div><button className="button primary" onClick={() => setShowServerForm(v => !v)}>＋ سرور جدید</button></div>{showServerForm && <form className="inline-form" onSubmit={addServer}><label>نام سرور<input value={serverName} onChange={e=>setServerName(e.target.value)} placeholder="مثلاً ZEUS • Istanbul" required /></label><label>منطقه<input value={serverRegion} onChange={e=>setServerRegion(e.target.value)} placeholder="مثلاً ترکیه" required /></label><button className="button primary">افزودن به فهرست</button></form>}<div className="table-wrap"><table><thead><tr><th>نام نود</th><th>منطقه / IP</th><th>پروتکل</th><th>بار</th><th>کاربران</th><th>وضعیت</th></tr></thead><tbody>{servers.map(s=><tr key={s.id}><td><b>{s.name}</b></td><td>{s.region}<small className="cell-sub">{s.ip}</small></td><td>{s.protocol}</td><td>{s.status==='pending'?'—':s.load+'%'}</td><td>{s.users}</td><td><Status value={s.status}/></td></tr>)}</tbody></table></div></section>}

      {page === 'users' && <section className="panel glass full-panel"><div className="panel-heading"><div><h2>مدیریت کاربران</h2><p>جست‌وجو و تغییر وضعیت نمونه کاربران</p></div><div className="search-box">⌕ <input value={query} onChange={e=>setQuery(e.target.value)} placeholder="جست‌وجوی نام یا شناسه..." /></div></div><div className="table-wrap"><table><thead><tr><th>شناسه</th><th>کاربر</th><th>اشتراک</th><th>مصرف</th><th>انقضا</th><th>وضعیت</th><th>عملیات</th></tr></thead><tbody>{filteredUsers.map(u=><tr key={u.id}><td>{u.id}</td><td><b>{u.name}</b></td><td>{u.plan}</td><td>{u.usage}</td><td>{u.expiry}</td><td><Status value={u.status}/></td><td><button className="mini-action" onClick={()=>toggleUser(u.id)}>{u.status==='active'?'غیرفعال‌سازی':'فعال‌سازی'}</button></td></tr>)}</tbody></table>{filteredUsers.length===0&&<div className="empty">کاربری با این مشخصات پیدا نشد.</div>}</div></section>}

      {page === 'plans' && <section className="plan-grid">{[['STARTER','۷ روزه','برای استفاده کوتاه‌مدت','۷ روز','۵۰ GB'],['PRO','۳۰ روزه','برای استفاده روزمره','۳۰ روز','نامحدود'],['ULTRA','۹۰ روزه','برای کاربران حرفه‌ای','۹۰ روز','نامحدود']].map((p,i)=><article className="plan-card glass" key={p[0]}><span className="plan-index">۰{i+1}</span><h2>{p[0]}</h2><h3>{p[1]}</h3><p>{p[2]}</p><div className="plan-detail"><span>مدت اعتبار</span><b>{p[3]}</b></div><div className="plan-detail"><span>سقف ترافیک</span><b>{p[4]}</b></div><button className="button secondary" onClick={()=>toast('تعریف و قیمت‌گذاری اشتراک‌ها پس از اتصال API فعال می‌شود.')}>ویرایش طرح</button></article>)}</section>}

      {page === 'activity' && <section className="panel glass full-panel"><div className="panel-heading"><div><h2>گزارش رویدادها</h2><p>رخدادهای نمونه؛ لاگ واقعی هنوز به پنل متصل نیست</p></div><span className="live-indicator"><i/> DEMO LOG</span></div><div className="activity-list large"><div><i className="activity-icon green">✓</i><span><b>Health check موفق برای Frankfurt</b><small>۲۰۲۶/۱۱/۱۳ · ۰۹:۴۲ · سیستم پایش</small></span><Status value="online"/></div><div><i className="activity-icon purple">♙</i><span><b>ورود مدیر نمونه</b><small>۲۰۲۶/۱۱/۱۳ · ۰۹:۲۱ · پنل محلی</small></span><Status value="active"/></div><div><i className="activity-icon orange">!</i><span><b>نود Warsaw هنوز پیکربندی نشده</b><small>۲۰۲۶/۱۱/۱۳ · ۰۸:۵۵ · زیرساخت</small></span><Status value="pending"/></div></div></section>}

      {page === 'settings' && <section className="panel glass full-panel settings-panel"><div className="panel-heading"><div><h2>تنظیمات پنل</h2><p>ترجیحات رابط کاربری و رفتار نمایشی</p></div></div><div className="setting-row"><span><b>بروزرسانی خودکار رابط</b><small>در نسخه متصل، داده‌ها به‌صورت دوره‌ای از API خوانده می‌شوند.</small></span><button className={autoRefresh?'switch on':'switch'} onClick={()=>setAutoRefresh(v=>!v)} aria-label="تغییر بروزرسانی خودکار"><i/></button></div><div className="setting-row"><span><b>وضعیت Control Plane</b><small>{apiMessage} — این بررسی فقط GET /healthz را می‌خواند و به معنی اتصال داده‌های مدیریتی نیست.</small></span><button className="button secondary" onClick={checkControlPlane}>بررسی دوباره</button></div><div className="setting-row"><span><b>حالت فشرده</b><small>کاهش فاصله‌ها برای نمایش اطلاعات بیشتر.</small></span><button className={compact?'switch on':'switch'} onClick={()=>setCompact(v=>!v)} aria-label="تغییر حالت فشرده"><i/></button></div><div className="setting-row"><span><b>پوسته فعلی</b><small>{themes.find(t=>t.id===theme).name}</small></span><button className="button secondary" onClick={()=>setTheme(themes[(themes.findIndex(t=>t.id===theme)+1)%themes.length].id)}>تغییر پوسته</button></div><div className="notice-box">امنیت: این نسخه به API واقعی، احراز هویت یا داده تولیدی متصل نیست. پیش از اتصال عمومی باید ورود امن، نقش‌ها، CSRF/CORS، اعتبارسنجی ورودی و ثبت ممیزی پیاده‌سازی شوند.</div></section>}

      <footer className="footer"><span>© ۲۰۲۶ ZEUS VPN PLATFORM</span><span>ساخته‌شده برای کنترل دقیق‌تر <i>✦</i></span><span className="footer-version">FRONTEND PREVIEW 0.1.0</span></footer>
    </main>
    {notice && <div className="toast" role="status">✦ {notice}</div>}
  </div>
}
