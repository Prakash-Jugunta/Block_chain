// Chart.js visualizations for ForkRisk Web Dashboard

let forkStaleChart = null;
let propDelayChart = null;
let goodputConsistencyChart = null;
let sensitivityChart = null;

const chartColors = {
  cyan: '#06b6d4',
  blue: '#3b82f6',
  purple: '#8b5cf6',
  emerald: '#10b981',
  amber: '#f59e0b',
  rose: '#f43f5e',
  textMuted: '#94a3b8',
  border: '#24324a'
};

function formatScenarioLabel(name) {
  return name.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase());
}

function initScenarioCharts(scenarios) {
  if (!window.Chart || !scenarios || scenarios.length === 0) return;

  const labels = scenarios.map(s => formatScenarioLabel(s.scenario_name));
  const forkRates = scenarios.map(s => (s.fork_rate_mean * 100).toFixed(2));
  const staleRates = scenarios.map(s => (s.stale_rate_mean * 100).toFixed(2));
  const meanDelays = scenarios.map(s => s.prop_delay_mean.toFixed(3));
  const p95Delays = scenarios.map(s => s.prop_delay_p95.toFixed(3));
  const goodputs = scenarios.map(s => s.goodput_mean.toFixed(2));
  const consistencies = scenarios.map(s => (s.node_consistency_mean * 100).toFixed(1));

  // Chart 1: Fork Rate vs Stale Rate
  const ctx1 = document.getElementById('chart-fork-stale');
  if (ctx1) {
    if (forkStaleChart) forkStaleChart.destroy();
    forkStaleChart = new Chart(ctx1, {
      type: 'bar',
      data: {
        labels: labels,
        datasets: [
          {
            label: 'Fork Rate (%)',
            data: forkRates,
            backgroundColor: 'rgba(244, 63, 94, 0.75)',
            borderColor: chartColors.rose,
            borderWidth: 1,
            borderRadius: 4
          },
          {
            label: 'Stale Block Rate (%)',
            data: staleRates,
            backgroundColor: 'rgba(245, 158, 11, 0.75)',
            borderColor: chartColors.amber,
            borderWidth: 1,
            borderRadius: 4
          }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { labels: { color: chartColors.textMuted } },
          tooltip: { mode: 'index', intersect: false }
        },
        scales: {
          x: { ticks: { color: chartColors.textMuted }, grid: { color: 'rgba(255,255,255,0.05)' } },
          y: {
            title: { display: true, text: 'Percentage (%)', color: chartColors.textMuted },
            ticks: { color: chartColors.textMuted },
            grid: { color: 'rgba(255,255,255,0.05)' }
          }
        }
      }
    });
  }

  // Chart 2: Propagation Latency
  const ctx2 = document.getElementById('chart-prop-delay');
  if (ctx2) {
    if (propDelayChart) propDelayChart.destroy();
    propDelayChart = new Chart(ctx2, {
      type: 'bar',
      data: {
        labels: labels,
        datasets: [
          {
            label: 'Mean Propagation Delay (s)',
            data: meanDelays,
            backgroundColor: 'rgba(6, 182, 212, 0.75)',
            borderColor: chartColors.cyan,
            borderWidth: 1,
            borderRadius: 4
          },
          {
            label: 'P95 Propagation Delay (s)',
            data: p95Delays,
            backgroundColor: 'rgba(139, 92, 246, 0.75)',
            borderColor: chartColors.purple,
            borderWidth: 1,
            borderRadius: 4
          }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { labels: { color: chartColors.textMuted } },
          tooltip: { mode: 'index', intersect: false }
        },
        scales: {
          x: { ticks: { color: chartColors.textMuted }, grid: { color: 'rgba(255,255,255,0.05)' } },
          y: {
            title: { display: true, text: 'Seconds (s)', color: chartColors.textMuted },
            ticks: { color: chartColors.textMuted },
            grid: { color: 'rgba(255,255,255,0.05)' }
          }
        }
      }
    });
  }

  // Chart 3: Goodput & Consistency
  const ctx3 = document.getElementById('chart-goodput-consistency');
  if (ctx3) {
    if (goodputConsistencyChart) goodputConsistencyChart.destroy();
    goodputConsistencyChart = new Chart(ctx3, {
      type: 'line',
      data: {
        labels: labels,
        datasets: [
          {
            label: 'Finalized Goodput (TPS)',
            data: goodputs,
            borderColor: chartColors.emerald,
            backgroundColor: 'rgba(16, 185, 129, 0.1)',
            fill: true,
            tension: 0.3,
            yAxisID: 'y'
          },
          {
            label: 'Node-View Consistency (%)',
            data: consistencies,
            borderColor: chartColors.blue,
            backgroundColor: 'rgba(59, 130, 246, 0.1)',
            fill: true,
            tension: 0.3,
            yAxisID: 'y1'
          }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        plugins: { legend: { labels: { color: chartColors.textMuted } } },
        scales: {
          x: { ticks: { color: chartColors.textMuted }, grid: { color: 'rgba(255,255,255,0.05)' } },
          y: {
            type: 'linear',
            display: true,
            position: 'left',
            title: { display: true, text: 'TPS', color: chartColors.emerald },
            ticks: { color: chartColors.emerald },
            grid: { color: 'rgba(255,255,255,0.05)' }
          },
          y1: {
            type: 'linear',
            display: true,
            position: 'right',
            title: { display: true, text: 'Consistency (%)', color: chartColors.blue },
            ticks: { color: chartColors.blue },
            grid: { drawOnChartArea: false }
          }
        }
      }
    });
  }
}

function initSensitivityChart(horizons) {
  if (!window.Chart || !horizons || horizons.length === 0) return;

  const labels = horizons.map(h => `H = ${h.horizon}`);
  const lrROC = horizons.map(h => h.lr_roc_auc != null ? h.lr_roc_auc.toFixed(4) : null);
  const rfROC = horizons.map(h => h.rf_roc_auc != null ? h.rf_roc_auc.toFixed(4) : null);
  const prevalence = horizons.map(h => (h.prevalence * 100).toFixed(1));

  const ctx = document.getElementById('chart-sensitivity');
  if (ctx) {
    if (sensitivityChart) sensitivityChart.destroy();
    sensitivityChart = new Chart(ctx, {
      type: 'line',
      data: {
        labels: labels,
        datasets: [
          {
            label: 'Logistic Regression ROC-AUC',
            data: lrROC,
            borderColor: chartColors.cyan,
            backgroundColor: 'rgba(6, 182, 212, 0.15)',
            borderWidth: 2,
            pointRadius: 5,
            tension: 0.2
          },
          {
            label: 'Random Forest ROC-AUC',
            data: rfROC,
            borderColor: chartColors.purple,
            backgroundColor: 'rgba(139, 92, 246, 0.15)',
            borderWidth: 2,
            pointRadius: 5,
            tension: 0.2
          },
          {
            label: 'Target Prevalence (%)',
            data: prevalence,
            borderColor: chartColors.amber,
            borderDash: [5, 5],
            borderWidth: 1.5,
            pointRadius: 3,
            tension: 0.2,
            yAxisID: 'y1'
          }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { labels: { color: chartColors.textMuted } },
          tooltip: { mode: 'index', intersect: false }
        },
        scales: {
          x: { ticks: { color: chartColors.textMuted }, grid: { color: 'rgba(255,255,255,0.05)' } },
          y: {
            min: 0.6,
            max: 0.95,
            title: { display: true, text: 'ROC-AUC Score', color: chartColors.textMuted },
            ticks: { color: chartColors.textMuted },
            grid: { color: 'rgba(255,255,255,0.05)' }
          },
          y1: {
            position: 'right',
            min: 20,
            max: 80,
            title: { display: true, text: 'Prevalence (%)', color: chartColors.amber },
            ticks: { color: chartColors.amber },
            grid: { drawOnChartArea: false }
          }
        }
      }
    });
  }
}
