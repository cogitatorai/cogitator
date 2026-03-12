import { useRef, useEffect, useCallback } from 'react';
import * as d3 from 'd3';
import type { DailyTokenStats } from '../api';

const TIER_COLORS: Record<string, string> = {
  standard: '#f97316', // orange-500
  cheap: '#2dd4bf',    // teal-400
};

const FONT = '"Rajdhani", SFMono-Regular, Menlo, Monaco, Consolas, monospace';

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

interface Props {
  stats: DailyTokenStats[];
}

export default function TokenChart({ stats }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);

  const buildChart = useCallback(() => {
    const svg = d3.select(svgRef.current);
    const container = containerRef.current;
    if (!container || !svgRef.current) return;

    const fullWidth = container.clientWidth;
    const fullHeight = container.clientHeight;
    svg.attr('width', fullWidth).attr('height', fullHeight);
    svg.selectAll('*').remove();
    d3.select(container).selectAll('[data-chart-tooltip]').remove();

    if (stats.length === 0) return;

    const margin = { top: 36, right: 20, bottom: 52, left: 68 };
    const width = fullWidth - margin.left - margin.right;
    const height = fullHeight - margin.top - margin.bottom;
    if (width <= 0 || height <= 0) return;

    // Dot grid background pattern
    const defs = svg.append('defs');
    const gridPattern = defs.append('pattern')
      .attr('id', 'chart-dots')
      .attr('width', 24)
      .attr('height', 24)
      .attr('patternUnits', 'userSpaceOnUse');
    gridPattern.append('circle')
      .attr('cx', 12).attr('cy', 12).attr('r', 0.6)
      .attr('fill', cssVar('--color-zinc-800'));

    svg.append('rect')
      .attr('width', fullWidth)
      .attr('height', fullHeight)
      .attr('fill', 'url(#chart-dots)');

    // Group stats by date
    const dates = [...new Set(stats.map(s => s.date))].sort();
    const tiers = [...new Set(stats.map(s => s.model_tier))];

    // Build lookup: date -> tier -> stats
    const lookup = new Map<string, Map<string, DailyTokenStats>>();
    for (const s of stats) {
      if (!lookup.has(s.date)) lookup.set(s.date, new Map());
      lookup.get(s.date)!.set(s.model_tier, s);
    }

    // Scales
    const x0 = d3.scaleBand().domain(dates).range([0, width]).paddingInner(0.3).paddingOuter(0.15);
    const x1 = d3.scaleBand().domain(tiers).range([0, x0.bandwidth()]).padding(0.1);
    const maxTokens = d3.max(stats, s => s.tokens_in + s.tokens_out) || 1;
    const y = d3.scaleLinear().domain([0, maxTokens * 1.1]).nice().range([height, 0]);

    const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`);

    // Dashed grid lines
    g.append('g')
      .attr('class', 'grid')
      .call(d3.axisLeft(y).tickSize(-width).tickFormat(() => ''))
      .call(sel => sel.select('.domain').remove())
      .call(sel => sel.selectAll('line')
        .attr('stroke', cssVar('--color-zinc-700'))
        .attr('stroke-opacity', 0.4)
        .attr('stroke-dasharray', '3 4'));

    // X axis
    g.append('g')
      .attr('transform', `translate(0,${height})`)
      .call(d3.axisBottom(x0).tickFormat(d => {
        const parts = (d as string).split('-');
        return `${parts[1]}/${parts[2]}`;
      }))
      .call(sel => {
        sel.select('.domain').attr('stroke', cssVar('--color-zinc-700')).attr('stroke-dasharray', '3 4');
        sel.selectAll('text')
          .attr('fill', cssVar('--color-zinc-400'))
          .attr('font-family', FONT)
          .attr('font-size', 12)
          .attr('letter-spacing', '0.08em')
          .attr('transform', 'rotate(-40)')
          .attr('text-anchor', 'end')
          .attr('dx', '-0.5em')
          .attr('dy', '0.25em');
        sel.selectAll('line').attr('stroke', cssVar('--color-zinc-700'));
      });

    // Y axis
    g.append('g')
      .call(d3.axisLeft(y).ticks(5).tickFormat(d => {
        const v = d as number;
        if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
        if (v >= 1_000) return `${(v / 1_000).toFixed(0)}k`;
        return String(v);
      }))
      .call(sel => {
        sel.select('.domain').attr('stroke', cssVar('--color-zinc-700')).attr('stroke-dasharray', '3 4');
        sel.selectAll('text')
          .attr('fill', cssVar('--color-zinc-400'))
          .attr('font-family', FONT)
          .attr('font-size', 12)
          .attr('letter-spacing', '0.08em');
        sel.selectAll('line').attr('stroke', cssVar('--color-zinc-700'));
      });

    // Tooltip (HUD style)
    const tooltip = d3.select(container)
      .append('div')
      .attr('data-chart-tooltip', '')
      .style('position', 'absolute')
      .style('pointer-events', 'none')
      .style('opacity', '0')
      .style('background', cssVar('--color-zinc-900') + 'f2')
      .style('border', '1px dashed ' + cssVar('--color-zinc-700'))
      .style('padding', '10px 14px')
      .style('font-family', FONT)
      .style('font-size', '13px')
      .style('color', cssVar('--color-zinc-400'))
      .style('z-index', '50')
      .style('transition', 'opacity 150ms');

    // Bars
    for (const date of dates) {
      const dateGroup = g.append('g').attr('transform', `translate(${x0(date)},0)`);
      for (const tier of tiers) {
        const s = lookup.get(date)?.get(tier);
        if (!s) continue;
        const total = s.tokens_in + s.tokens_out;
        const color = TIER_COLORS[tier] || '#71717a';

        dateGroup.append('rect')
          .attr('x', x1(tier)!)
          .attr('y', y(total))
          .attr('width', x1.bandwidth())
          .attr('height', Math.max(0, height - y(total)))
          .attr('fill', color)
          .attr('opacity', 0.8)
          .attr('rx', 1)
          .style('cursor', 'pointer')
          .on('mouseenter', function (event) {
            d3.select(this).attr('opacity', 1);
            const parts = date.split('-');
            const headColor = cssVar('--color-zinc-100');
            const borderColor = cssVar('--color-zinc-700');
            tooltip.style('opacity', '1').html(
              `<div style="color:${headColor};font-weight:700;font-size:13px;text-transform:uppercase;letter-spacing:0.15em;margin-bottom:6px">${parts[1]}/${parts[2]}/${parts[0]}</div>` +
              `<div style="color:${color};font-size:12px;text-transform:uppercase;letter-spacing:0.2em;margin-bottom:6px">${tier}</div>` +
              `<div style="margin-bottom:2px">In:  ${s.tokens_in.toLocaleString()}</div>` +
              `<div>Out: ${s.tokens_out.toLocaleString()}</div>` +
              `<div style="color:${headColor};margin-top:6px;padding-top:4px;border-top:1px dashed ${borderColor}">Total: ${total.toLocaleString()}</div>`
            );
            const rect = container.getBoundingClientRect();
            tooltip
              .style('left', (event.clientX - rect.left + 14) + 'px')
              .style('top', (event.clientY - rect.top - 14) + 'px');
          })
          .on('mousemove', function (event) {
            const rect = container.getBoundingClientRect();
            tooltip
              .style('left', (event.clientX - rect.left + 14) + 'px')
              .style('top', (event.clientY - rect.top - 14) + 'px');
          })
          .on('mouseleave', function () {
            d3.select(this).attr('opacity', 0.8);
            tooltip.style('opacity', '0');
          });
      }
    }

    // Legend (top-right, matches HUD style)
    const legend = svg.append('g')
      .attr('transform', `translate(${fullWidth - margin.right - tiers.length * 100},${10})`);

    tiers.forEach((tier, i) => {
      const lg = legend.append('g').attr('transform', `translate(${i * 100},0)`);
      lg.append('rect')
        .attr('width', 10).attr('height', 10)
        .attr('fill', TIER_COLORS[tier] || '#71717a')
        .attr('opacity', 0.85)
        .attr('rx', 1);
      lg.append('text')
        .attr('x', 16).attr('y', 9)
        .attr('fill', cssVar('--color-zinc-400'))
        .attr('font-family', FONT)
        .attr('font-size', 12)
        .attr('letter-spacing', '0.15em')
        .text(tier.toUpperCase());
    });

    return () => { tooltip.remove(); };
  }, [stats]);

  useEffect(() => {
    const cleanup = buildChart();
    return () => { if (cleanup) cleanup(); };
  }, [buildChart]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const observer = new ResizeObserver(() => buildChart());
    observer.observe(container);
    return () => observer.disconnect();
  }, [buildChart]);

  return (
    <div ref={containerRef} className="relative w-full h-full min-h-[320px]">
      <svg ref={svgRef} className="w-full h-full" />
      {stats.length === 0 && (
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-600">
            No token usage data yet
          </span>
        </div>
      )}
    </div>
  );
}
