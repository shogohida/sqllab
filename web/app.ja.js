// sqllab frontend (Japanese UI). Same logic as app.js — no build step, no
// framework — just with UI strings translated and scenario titles/
// descriptions localized client-side (the API itself still returns English
// scenario metadata, since it's shared with the English page).

const sqlBox = document.getElementById('sqlBox');
const runBtn = document.getElementById('runBtn');
const addIndexBtn = document.getElementById('addIndexBtn');
const resetBtn = document.getElementById('resetBtn');
const fixExplanation = document.getElementById('fixExplanation');
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

// シナリオを選択していないときは、実際に最後に実行したSQLを
// /api/suggest-index にかけた結果で「修正」ボタンを駆動する
// （固定シナリオの手書きの提案とは別系統）。
let dynamicSuggestion = null;
let lastRunSQL = '';

// Scenario titles/descriptions/example questions come back from the API in
// English (they're shared with the English page); this maps them to
// Japanese for display purposes only. The underlying SQL is unchanged.
const SCENARIO_JA = {
  'customer-order-history': {
    title: '顧客の注文履歴',
    description: 'カスタマーサポート担当者が、ある顧客の全注文を新しい順に取得します——ECバックエンドで最も一般的な検索の一つです。',
    askAiPrompt: '顧客42番の注文をすべて、新しい順に表示して',
    fixExplanation: '（customer_id, order_date）の複合インデックスにより、SQLiteはこの顧客の行だけをすでに日付順の状態で直接たどれます——スキャンも別途のソートも不要です。',
  },
  'order-line-items': {
    title: '注文明細',
    description: '注文の詳細ページを表示するには、その注文に属するすべての明細行を取得する必要があります。',
    askAiPrompt: '注文777番の明細をすべてリストして',
    fixExplanation: 'order_idにインデックスを張ることで、テーブル全体のスキャンがこの注文の行への直接ルックアップに変わります。',
  },
  'regional-revenue': {
    title: '期間指定の地域別売上',
    description: '経理部門が、特定の四半期における都市別の完了済み売上を求めます——他の3テーブルすべてを結合し、絞り込んでグループ化するクエリです。',
    askAiPrompt: '2025年第3四半期の完了済み売上を都市別に教えて',
    fixExplanation: '（status, order_date）の複合インデックスにより、JOINが始まる前にordersのスキャン範囲をこの四半期の完了済み行だけに絞り込めます。',
  },
  'product-search': {
    title: 'カテゴリと価格による商品検索',
    description: 'ストアフロントが、カテゴリと上限価格で商品を絞り込み、安い順に表示します——典型的なファセット検索クエリです。',
    askAiPrompt: '100ドル未満のElectronicsカテゴリの商品を、安い順に探して',
    fixExplanation: '（category, price）の複合インデックスにより、SQLiteはElectronicsカテゴリで100ドル未満の商品に直接ジャンプでき、しかもすでに価格順になっています。',
  },
  'unsargable-date-filter': {
    title: '日付列を関数で包んだ月次フィルタ',
    description: 'レポート画面が「特定の月に発生した注文」を求めます。order_dateをstrftime()で年月に変換して比較すると、たとえインデックスを追加してもSQLiteはそのインデックスを使えなくなります。',
    askAiPrompt: '2025年8月に発生した注文をすべて見せて',
    fixExplanation: 'order_dateにインデックスを追加するだけでは効果がありません——列を関数呼び出しで包んでいるとSQLiteはインデックスを使えず、結局は全行をスキャンします。フィルタを（結果は同じままの）単純な範囲比較に書き換えることで、初めてそのインデックスが使われるようになります。',
  },
  'deep-pagination': {
    title: '注文一覧の深いページネーション',
    description: '注文一覧で2001ページ目にジャンプするため、OFFSETで4万行を読み飛ばします。order_dateにインデックスがあっても、SQLiteは次のページを返す前に読み飛ばした行を一つずつ通過しなければなりません。',
    askAiPrompt: '日付順（新しい順）で最初の4万件を飛ばして、次の20件の注文を見せて',
    fixExplanation: 'インデックスによって並び替え自体はほぼ無料になりますが、OFFSETは読み飛ばす4万行を一つずつ数える必要があり、この部分のコストはLIMIT/OFFSETの仕組みでは避けられません。実運用のページネーションは、最後に見た行を覚えておいてそこから先を探す「キーセットページネーション」を使います。',
  },
};

function ja(s) {
  return SCENARIO_JA[s.id] || {
    title: s.title,
    description: s.description,
    askAiPrompt: s.ask_ai_prompt,
    fixExplanation: s.fix_explanation,
  };
}

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
  if (!res.ok) throw new Error(data.error || `リクエストが失敗しました（${res.status}）`);
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
    const t = ja(s);
    el.innerHTML = `${t.title}<span class="desc">${t.description}</span>`;
    el.addEventListener('click', () => selectScenario(s, el));
    scenarioList.appendChild(el);
  });
}

function fixButtonLabel(s) {
  if (s.rewritten_query) {
    return s.suggested_index_sql
      ? '修正を適用（インデックス＋クエリ書き換え）'
      : '修正を適用（クエリ書き換え）';
  }
  return `推奨インデックスを追加（${s.suggested_index_sql}）`;
}

function selectScenario(s, el) {
  document.querySelectorAll('.scenario').forEach(b => b.classList.remove('active'));
  el.classList.add('active');
  activeScenario = s;
  dynamicSuggestion = null;
  sqlBox.value = s.query;
  scenarioDesc.textContent = ja(s).description;
  fixExplanation.textContent = ja(s).fixExplanation || '';
  addIndexBtn.disabled = false;
  addIndexBtn.textContent = fixButtonLabel(s);
}

// 実際に直前に実行されたクエリ（手入力またはAI生成）を
// /api/suggest-index で解析し、その結果で「修正」ボタンを更新する
// ——これにより固定シナリオ以外の任意のSELECTでも「修正を適用」が使えるようになる。
async function refreshSuggestion(sql) {
  dynamicSuggestion = null;
  try {
    dynamicSuggestion = await api('/api/suggest-index', { body: { sql } });
  } catch (e) {
    dynamicSuggestion = { reason: e.message };
  }

  if (dynamicSuggestion.suggested_index_sql) {
    addIndexBtn.disabled = false;
    addIndexBtn.textContent = `推奨インデックスを追加（${dynamicSuggestion.suggested_index_sql}）`;
  } else {
    addIndexBtn.disabled = true;
    addIndexBtn.textContent = '推奨インデックスを追加';
  }
  fixExplanation.textContent = dynamicSuggestion.reason || '';
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
  ).join('') || '<div class="hint">プランなし（SELECT文ではありません）。</div>';

  statsBox.innerHTML = `
    <span>経過時間: <b>${result.elapsed_ms.toFixed(2)} ms</b></span>
    ${result.row_count !== undefined ? `<span>行数: <b>${result.row_count}${result.truncated ? '+' : ''}</b></span>` : ''}
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
    log('ok', `${logLabel || result.kind} → ${result.elapsed_ms.toFixed(2)}ms${result.row_count !== undefined ? `、${result.row_count}行` : ''}`);
    return result;
  } catch (e) {
    log('err', `${logLabel || 'クエリ'}が失敗しました: ${e.message}`);
    throw e;
  }
}

// シナリオのクエリを手で編集すると、固定シナリオとの紐付けが切れる
// ——以後「修正」ボタンは実際にボックスにあるSQLの動的解析で駆動される。
sqlBox.addEventListener('input', () => {
  if (activeScenario && sqlBox.value.trim() !== activeScenario.query.trim()) {
    activeScenario = null;
    dynamicSuggestion = null;
    addIndexBtn.disabled = true;
    addIndexBtn.textContent = '推奨インデックスを追加';
    fixExplanation.textContent = '';
  }
});

runBtn.addEventListener('click', async () => {
  const sql = sqlBox.value.trim();
  if (!sql) return;
  const result = await runSQL(sql, { logLabel: '実行' }).catch(() => null);
  if (result && result.kind === 'select' && !activeScenario) {
    lastRunSQL = sql;
    await refreshSuggestion(sql);
  }
});

addIndexBtn.addEventListener('click', async () => {
  try {
    if (activeScenario) {
      if (activeScenario.suggested_index_sql) {
        await runSQL(activeScenario.suggested_index_sql, { logLabel: 'インデックス追加' });
      }
      addIndexBtn.disabled = true;
      // Immediately rerun the (possibly rewritten) query so the before/after
      // is visible without an extra click.
      const finalQuery = activeScenario.rewritten_query || activeScenario.query;
      sqlBox.value = finalQuery;
      await runSQL(finalQuery, { logLabel: activeScenario.rewritten_query ? '書き換え後のクエリを実行' : 'インデックス追加後に再実行' });
    } else if (dynamicSuggestion && dynamicSuggestion.suggested_index_sql) {
      await runSQL(dynamicSuggestion.suggested_index_sql, { logLabel: 'インデックス追加' });
      addIndexBtn.disabled = true;
      await runSQL(lastRunSQL, { logLabel: 'インデックス追加後に再実行' });
    }
  } catch {
    // runSQL already logged the failure.
  }
});

resetBtn.addEventListener('click', async () => {
  document.cookie = 'sqllab_session=; Max-Age=0; path=/';
  planBox.innerHTML = '';
  statsBox.innerHTML = '';
  resultsBox.innerHTML = '';
  dynamicSuggestion = null;
  lastRunSQL = '';
  if (activeScenario) {
    sqlBox.value = activeScenario.query;
    addIndexBtn.disabled = false;
    addIndexBtn.textContent = fixButtonLabel(activeScenario);
  } else {
    addIndexBtn.disabled = true;
    addIndexBtn.textContent = '推奨インデックスを追加';
    fixExplanation.textContent = '';
  }
  log('ok', 'サンドボックスをリセットしました — 次のクエリで新しいセッションが開始されます');
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
  const fewShot = scenarioList.map(s => `Q: ${ja(s).askAiPrompt}\nSQL: ${s.query}`).join('\n\n');
  return [
    'You translate natural-language questions (which may be in Japanese or English) into a single SQLite SELECT statement.',
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
  loadModelBtn.title = 'WebGPU対応ブラウザが必要です（例: デスクトップ版Chrome、Edgeなど）。';
  aiStatus.textContent = 'このブラウザはWebGPUに対応していないため、ブラウザ内AIモデルは実行できません。ページの他の機能は問題なくご利用いただけます。';
}

loadModelBtn.addEventListener('click', async () => {
  loadModelBtn.disabled = true;
  aiStatus.textContent = 'WebLLMを読み込み中…';
  try {
    const webllm = await import('https://esm.run/@mlc-ai/web-llm');
    const modelId = pickModel(webllm.prebuiltAppConfig.model_list);
    aiStatus.textContent = `${modelId} を読み込み中…`;

    webllmEngine = await webllm.CreateMLCEngine(modelId, {
      initProgressCallback: (report) => {
        aiStatus.textContent = report.text;
      },
    });

    aiStatus.textContent = `準備完了（${modelId}）。ブラウザ内でローカル実行中です。`;
    aiQuestion.disabled = false;
    askAiBtn.disabled = false;
  } catch (e) {
    aiStatus.textContent = `AIモデルを読み込めませんでした: ${e.message}`;
    loadModelBtn.disabled = false;
  }
});

askAiBtn.addEventListener('click', async () => {
  const question = aiQuestion.value.trim();
  if (!question || !webllmEngine) return;

  askAiBtn.disabled = true;
  aiStatus.textContent = 'SQLを生成中…';
  try {
    const completion = await webllmEngine.chat.completions.create({
      messages: [
        { role: 'system', content: buildSystemPrompt(schemaTables, scenarios) },
        { role: 'user', content: question },
      ],
      temperature: 0.1,
    });
    const sql = extractSQL(completion.choices[0].message.content);
    activeScenario = null;
    document.querySelectorAll('.scenario').forEach(b => b.classList.remove('active'));
    sqlBox.value = sql;
    aiStatus.textContent = `生成結果: ${sql}`;
    log('ok', `「${question}」に対してAIがSQLを生成しました`);
    const result = await runSQL(sql, { logLabel: 'AI生成クエリ' });
    if (result && result.kind === 'select') {
      lastRunSQL = sql;
      await refreshSuggestion(sql);
    }
  } catch (e) {
    aiStatus.textContent = `生成に失敗しました: ${e.message}`;
    log('err', `AI生成に失敗しました: ${e.message}`);
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
    log('err', `スキーマ／シナリオの読み込みに失敗しました: ${e.message}`);
  }
})();
