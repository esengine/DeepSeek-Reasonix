package usage

// dashboardHTML is the self-contained HTML/CSS/JS for the usage dashboard.
// It loads Chart.js from CDN and fetches data from /api/* endpoints.
const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Reasonix Usage Dashboard</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f1117;color:#e4e4e7;padding:24px}
h1{font-size:1.5rem;margin-bottom:8px;color:#fff}
h2{font-size:1.1rem;margin:24px 0 12px;color:#a1a1aa}
.controls{display:flex;gap:8px;margin-bottom:24px}
.controls button{padding:6px 16px;border:1px solid #3f3f46;background:#18181b;color:#e4e4e7;border-radius:6px;cursor:pointer;font-size:.85rem}
.controls button.active{background:#3b82f6;border-color:#3b82f6;color:#fff}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:16px;margin-bottom:24px}
.card{background:#18181b;border-radius:12px;padding:20px;border:1px solid #27272a}
.card .label{font-size:.8rem;color:#71717a;margin-bottom:4px}
.card .value{font-size:1.6rem;font-weight:700;color:#fff}
.chart-row{display:grid;grid-template-columns:2fr 1fr;gap:16px;margin-bottom:24px}
@media(max-width:800px){.chart-row{grid-template-columns:1fr}}
.chart-box{background:#18181b;border-radius:12px;padding:16px;border:1px solid #27272a}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:8px 12px;border-bottom:1px solid #27272a;font-size:.85rem}
th{color:#71717a;font-weight:500}
td{color:#e4e4e7}
.footer{margin-top:32px;text-align:center;color:#52525b;font-size:.75rem}
</style>
</head>
<body>
<h1>Reasonix Usage Dashboard</h1>
<div class="controls">
  <button data-days="0" onclick="setDays(0)">Today</button>
  <button data-days="7" onclick="setDays(7)">7d</button>
  <button data-days="30" onclick="setDays(30)">30d</button>
  <button data-days="90" onclick="setDays(90)">90d</button>
</div>

<div class="cards" id="cards"></div>

<div class="chart-row">
  <div class="chart-box"><canvas id="trendChart"></canvas></div>
  <div class="chart-box"><canvas id="costChart"></canvas></div>
</div>

<h2>Request Log</h2>
<table>
  <thead><tr><th>Time</th><th>Model</th><th>Source</th><th>Prompt</th><th>Completion</th><th>Cache Hit</th><th>Cost</th></tr></thead>
  <tbody id="logBody"></tbody>
</table>

<div class="footer">Reasonix Usage Dashboard &middot; Data from ~/.reasonix/usage/</div>

<script>
let days = 0;
let trendChart, costChart;

function fmt(n){return n.toLocaleString()}
function esc(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;')}

function setDays(d){
  days=d;
  document.querySelectorAll('.controls button').forEach(b=>b.classList.toggle('active',+b.dataset.days===d));
  refresh();
}

async function refresh(){
  const[ov,trend,logs]=await Promise.all([
    fetch('/api/overview?days='+days).then(r=>r.json()),
    fetch('/api/trend?days='+days).then(r=>r.json()),
    fetch('/api/logs?days='+days+'&limit=50').then(r=>r.json()),
  ]);
  renderCards(ov);
  renderTrend(trend);
  renderCost(ov.models||[]);
  renderLogs(logs);
}

function renderCards(ov){
  const o=ov.overview||ov;
  const hitRate=o.cache_hit_tokens+o.cache_miss_tokens>0?(o.cache_hit_tokens/(o.cache_hit_tokens+o.cache_miss_tokens)*100).toFixed(1)+'%':'N/A';
  document.getElementById('cards').innerHTML=[
    card('Total Tokens',fmt(o.total_tokens)),
    card('Cache Hit Rate',hitRate),
    card('Total Cost',(o.currency||'¥')+o.cost.toFixed(4)),
    card('Requests',fmt(o.requests)),
    card('TPM',o.tpm?o.tpm.toFixed(0):'N/A'),
    card('RPM',o.rpm?o.rpm.toFixed(1):'N/A'),
  ].join('');
}
function card(label,value){return '<div class="card"><div class="label">'+label+'</div><div class="value">'+value+'</div></div>'}

function renderTrend(data){
  const ctx=document.getElementById('trendChart');
  if(trendChart)trendChart.destroy();
  trendChart=new Chart(ctx,{
    type:'line',
    data:{
      labels:data.map(d=>d.date.slice(5)),
      datasets:[
        {label:'Prompt',data:data.map(d=>d.prompt_tokens),borderColor:'#3b82f6',fill:false,tension:.3},
        {label:'Completion',data:data.map(d=>d.completion_tokens),borderColor:'#10b981',fill:false,tension:.3},
        {label:'Cache Hit',data:data.map(d=>d.cache_hit_tokens),borderColor:'#f59e0b',fill:false,tension:.3},
      ]
    },
    options:{responsive:true,plugins:{title:{display:true,text:'Token Trend',color:'#e4e4e7'}},scales:{x:{ticks:{color:'#71717a'}},y:{ticks:{color:'#71717a'}}}}
  });
}

function renderCost(models){
  const ctx=document.getElementById('costChart');
  if(costChart)costChart.destroy();
  const colors=['#3b82f6','#10b981','#f59e0b','#ef4444','#8b5cf6','#ec4899'];
  costChart=new Chart(ctx,{
    type:'doughnut',
    data:{
      labels:models.map(m=>m.model),
      datasets:[{data:models.map(m=>m.cost),backgroundColor:colors.slice(0,models.length)}]
    },
    options:{responsive:true,plugins:{title:{display:true,text:'Cost by Model',color:'#e4e4e7'},legend:{labels:{color:'#e4e4e7'}}}}
  });
}

function renderLogs(data){
  const body=document.getElementById('logBody');
  body.innerHTML=(data||[]).map(e=>'<tr><td>'+esc(e.ts.slice(0,19))+'</td><td>'+esc((e.provider?e.provider+'/':'')+e.model)+'</td><td>'+esc(e.usage_source)+'</td><td>'+fmt(e.prompt_tokens)+'</td><td>'+fmt(e.completion_tokens)+'</td><td>'+fmt(e.cache_hit_tokens)+'</td><td>'+esc(e.currency||'¥')+e.cost.toFixed(4)+'</td></tr>').join('');
}

setDays(0);
</script>
</body>
</html>`
