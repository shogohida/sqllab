// sqllab frontend. No build step, no framework — plain ES modules, same
// spirit as the raftkv demo page. WebLLM is dynamically imported only when
// the visitor clicks "Load AI model," so the page never pulls in a large
// dependency (or downloads a ~1GB model) unless asked to.

const sqlBox = document.getElementById('sqlBox');
const runBtn = document.getElementById('runBtn');
const addIndexBtn = document.getElementById('addIndexBtn');
const resetBtn = document.getElementById('resetBtn');
const planBox = document.getElementById('planBox');
const statsBox = document.getElementById('statsBox');
const resultsBox = document.getElementById('resultsBox');
const scenarioList = document.getElementById('scenarioList');
const scenarioDesc = document.getElementById('scenarioDesc');
const schemaBrowser = document.getElementById('schemaBrowser');
const logEl = document.getElementById('log');

const loadModelBtn = document.getElementById('loadModelBtn');
const aiQuestion = document.getElementById('aiQuestion');
const askAiBtn = document.getElementById('askAiBtn');
const aiStatus = document.getElementById('aiStatus');

let scenarios = [];
let activeScenario = null;
let schemaTables = [];
let webllmEngine = null;

function log(kind, text) {
  const entry = document.createElement('div');
  entry.className = 'log-entry ' + kind;
  const time = new Date().toLocaleTimeString();
  entry.innerHTML = `<span class="meta">${time}</span>${text}`;
  logEl.prepend(entry);
}

async function api(path, opts) {
  const res = await fetch(path, {
    method: opts?.body ? 'POST' : 'GET',
    headers: opts?.body ? { 'Content-Type': 'application/json' } : undefined,
    body: opts?.body ? JSON.stringify(opts.body) : undefined,
    credentials: 'same-origin',
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `request failed (${res.status})`);
  return data;
}

function renderSchema(tables) {
  schemaBrowser.innerHTML = tables.map(t => `
    <div class="schema-table">
      <span class="tname">${t.name}</span>
      ${t.columns.map(c => `<div class="col">${c.name} <span style="opacity:.6">${c.type}</span></div>`).join('')}
    </div>
  `).join('');
}

function renderScenarios(list) {
  scenarioList.innerHTML = '';
  list.forEach(s => {
    const el = document.createElement('button');
    el.className = 'scenario';
    el.type = 'button';
    el.innerHTML = `${s.title}<span class="desc">${s.description}</span>`;
    el.addEventListener('click', () => selectScenario(s, el));
    scenarioList.appendChild(el);
  });
}

function selectScenario(s, el) {
  document.querySelectorAll('.scenario').forEach(b => b.classList.remove('active'));
  el.classList.add('active');
  activeScenario = s;
  sqlBox.value = s.query;
  scenarioDesc.textContent = s.description;
  addIndexBtn.disabled = false;
  addIndexBtn.textContent = `Add suggested index (${s.suggested_index_sql})`;
}

function planLineClass(line) {
  const upper = line.toUpperCase();
  if (upper.startsWith('SCAN')) return 'scan';
  if (upper.startsWith('SEARCH')) return 'search';
  return 'other';
}

function renderResult(result) {
  planBox.innerHTML = (result.plan || []).map(l =>
    `<div class="plan-line ${planLineClass(l)}">${l}</div>`
  ).join('') || '<div class="hint">No plan (not a SELECT).</div>';

  statsBox.innerHTML = `
    <span>Elapsed: <b>${result.elapsed_ms.toFixed(2)} ms</b></span>
    ${result.row_count !== undefined ? `<span>Rows: <b>${result.row_count}${result.truncated ? '+' : ''}</b></span>` : ''}
  `;

  if (result.columns && result.rows) {
    const head = `<tr>${result.columns.map(c => `<th>${c}</th>`).join('')}</tr>`;
    const body = result.rows.map(r => `<tr>${r.map(v => `<td>${v === null ? 'NULL' : String(v)}</td>`).join('')}</tr>`).join('');
    resultsBox.innerHTML = `<table class="results">${head}${body}</table>`;
  } else {
    resultsBox.innerHTML = '';
  }
}

async function runSQL(sql, { logLabel } = {}) {
  try {
    const result = await api('/api/query', { body: { sql } });
    renderResult(result);
    log('ok', `${logLabel || result.kind} → ${result.elapsed_ms.toFixed(2)}ms${result.row_count !== undefined ? `, ${result.row_count} rows` : ''}`);
    return result;
  } catch (e) {
    log('err', `${logLabel || 'query'} failed: ${e.message}`);
    throw e;
  }
}

runBtn.addEventListener('click', () => {
  const sql = sqlBox.value.trim();
  if (!sql) return;
  runSQL(sql, { logLabel: 'run' });
});

addIndexBtn.addEventListener('click', async () => {
  if (!activeScenario) return;
  try {
    await runSQL(activeScenario.suggested_index_sql, { logLabel: 'add index' });
    addIndexBtn.disabled = true;
    // Immediately rerun the same query so the before/after is visible without
    // an extra click.
    await runSQL(activeScenario.query, { logLabel: 're-run after index' });
  } catch {
    // runSQL already logged the failure.
  }
});

resetBtn.addEventListener('click', async () => {
  document.cookie = 'sqllab_session=; Max-Age=0; path=/';
  planBox.innerHTML = '';
  statsBox.innerHTML = '';
  resultsBox.innerHTML = '';
  log('ok', 'sandbox reset — next query starts a fresh session');
});

// --- WebLLM: natural-language → SQL, entirely client-side ---

const PREFERRED_MODELS = ['Qwen2.5-1.5B-Instruct', 'Llama-3.2-3B-Instruct', 'Qwen2.5-0.5B-Instruct'];

function pickModel(modelList) {
  for (const name of PREFERRED_MODELS) {
    const found = modelList.find(m => m.model_id.includes(name));
    if (found) return found.model_id;
  }
  return modelList[0]?.model_id;
}

function buildSystemPrompt(tables, scenarioList) {
  const ddl = tables.map(t =>
    `CREATE TABLE ${t.name} (${t.columns.map(c => `${c.name} ${c.type}`).join(', ')});`
  ).join('\n');
  const fewShot = scenarioList.map(s => `Q: ${s.ask_ai_prompt}\nSQL: ${s.query}`).join('\n\n');
  return [
    'You translate natural-language questions into a single SQLite SELECT statement.',
    'Schema:',
    ddl,
    '',
    'Examples:',
    fewShot,
    '',
    'Rules: output ONLY one SELECT statement ending in a semicolon. No markdown, no',
    'explanation, no comments. Only reference the tables and columns shown above.',
  ].join('\n');
}

function extractSQL(text) {
  let s = text.trim();
  const fence = s.match(/```(?:sql)?\s*([\s\S]*?)```/i);
  if (fence) s = fence[1].trim();
  return s;
}

if (!navigator.gpu) {
  loadModelBtn.disabled = true;
  loadModelBtn.title = 'Requires a WebGPU-capable browser (e.g. desktop Chrome or Edge).';
  aiStatus.textContent = 'Your browser does not support WebGPU, so the in-browser AI model can\'t run here. Everything else on this page still works.';
}

loadModelBtn.addEventListener('click', async () => {
  loadModelBtn.disabled = true;
  aiStatus.textContent = 'Loading WebLLM…';
  try {
    const webllm = await import('https://esm.run/@mlc-ai/web-llm');
    const modelId = pickModel(webllm.prebuiltAppConfig.model_list);
    aiStatus.textContent = `Loading ${modelId}…`;

    webllmEngine = await webllm.CreateMLCEngine(modelId, {
      initProgressCallback: (report) => {
        aiStatus.textContent = report.text;
      },
    });

    aiStatus.textContent = `Ready (${modelId}), running locally in your browser.`;
    aiQuestion.disabled = false;
    askAiBtn.disabled = false;
  } catch (e) {
    aiStatus.textContent = `Could not load the AI model: ${e.message}`;
    loadModelBtn.disabled = false;
  }
});

askAiBtn.addEventListener('click', async () => {
  const question = aiQuestion.value.trim();
  if (!question || !webllmEngine) return;

  askAiBtn.disabled = true;
  aiStatus.textContent = 'Generating SQL…';
  try {
    const completion = await webllmEngine.chat.completions.create({
      messages: [
        { role: 'system', content: buildSystemPrompt(schemaTables, scenarios) },
        { role: 'user', content: question },
      ],
      temperature: 0.1,
    });
    const sql = extractSQL(completion.choices[0].message.content);
    sqlBox.value = sql;
    aiStatus.textContent = `Generated: ${sql}`;
    log('ok', `AI generated SQL for "${question}"`);
    await runSQL(sql, { logLabel: 'AI-generated query' });
  } catch (e) {
    aiStatus.textContent = `Generation failed: ${e.message}`;
    log('err', `AI generation failed: ${e.message}`);
  } finally {
    askAiBtn.disabled = false;
  }
});

// --- init ---

(async function init() {
  try {
    [schemaTables, scenarios] = await Promise.all([
      api('/api/schema'),
      api('/api/scenarios'),
    ]);
    renderSchema(schemaTables);
    renderScenarios(scenarios);
    if (scenarios.length) {
      selectScenario(scenarios[0], scenarioList.querySelector('.scenario'));
    }
  } catch (e) {
    log('err', `failed to load schema/scenarios: ${e.message}`);
  }
})();
