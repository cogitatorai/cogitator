import { useRef, useEffect, useCallback } from 'react';
import * as d3 from 'd3';
import type { MemoryNodeSummary, MemoryEdge } from '../api';

// Muted type colors: used at low opacity for fills, full for thin accents.
const TYPE_ACCENT: Record<string, string> = {
  fact: '#60a5fa',
  preference: '#a78bfa',
  pattern: '#2dd4bf',
  skill: '#f97316',
  episode: '#4ade80',
  task_knowledge: '#fbbf24',
};

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

// All edges use the same subtle zinc tone; relation labels differentiate.
// Resolved at chart build time from CSS custom properties.
let EDGE_COLOR = '#3f3f46';

interface GraphNode extends d3.SimulationNodeDatum {
  id: string;
  type: string;
  title: string;
  summary: string;
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  relation: string;
  weight: number;
}

interface Props {
  nodes: MemoryNodeSummary[];
  edges: MemoryEdge[];
  selectedNodeId: string | null;
  onSelectNode: (id: string) => void;
}

export default function MemoryGraph({ nodes, edges, selectedNodeId, onSelectNode }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const simulationRef = useRef<d3.Simulation<GraphNode, GraphLink> | null>(null);

  const buildGraph = useCallback(() => {
    const svg = d3.select(svgRef.current);
    const container = containerRef.current;
    if (!container || !svgRef.current) return;

    EDGE_COLOR = cssVar('--color-zinc-700');

    const width = container.clientWidth;
    const height = container.clientHeight;

    svg.attr('width', width).attr('height', height);
    svg.selectAll('*').remove();
    d3.select(container).selectAll('[data-graph-tooltip]').remove();

    if (nodes.length === 0) return;

    const nodeMap = new Map(nodes.map(n => [n.id, true]));
    const graphNodes: GraphNode[] = nodes.map(n => ({ ...n }));
    const graphLinks: GraphLink[] = edges
      .filter(e => nodeMap.has(e.source_id) && nodeMap.has(e.target_id))
      .map(e => ({
        source: e.source_id,
        target: e.target_id,
        relation: e.relation,
        weight: e.weight,
      }));

    // Defs: dot grid pattern + arrow marker
    const defs = svg.append('defs');

    // Dot grid background pattern
    const gridPattern = defs.append('pattern')
      .attr('id', 'grid-dots')
      .attr('width', 24)
      .attr('height', 24)
      .attr('patternUnits', 'userSpaceOnUse');
    gridPattern.append('circle')
      .attr('cx', 12).attr('cy', 12).attr('r', 0.5)
      .attr('fill', cssVar('--color-zinc-800'));

    // Single arrow marker in zinc-600
    defs.append('marker')
      .attr('id', 'arrow')
      .attr('viewBox', '0 -4 8 8')
      .attr('refX', 24)
      .attr('refY', 0)
      .attr('markerWidth', 5)
      .attr('markerHeight', 5)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-3L8,0L0,3')
      .attr('fill', cssVar('--color-zinc-600'));

    // Background rect with dot grid
    svg.append('rect')
      .attr('width', width)
      .attr('height', height)
      .attr('fill', 'url(#grid-dots)');

    const g = svg.append('g');

    // Zoom
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.3, 4])
      .on('zoom', (event) => g.attr('transform', event.transform));
    (svg as unknown as d3.Selection<SVGSVGElement, unknown, null, undefined>).call(zoom);

    // Edges
    const link = g.append('g')
      .selectAll('line')
      .data(graphLinks)
      .join('line')
      .attr('stroke', EDGE_COLOR)
      .attr('stroke-opacity', 0.6)
      .attr('stroke-width', 1)
      .attr('stroke-dasharray', d => d.relation === 'contradicts' ? '4 3' : 'none')
      .attr('marker-end', 'url(#arrow)');

    // Edge labels (hidden until hover)
    const edgeLabel = g.append('g')
      .selectAll('text')
      .data(graphLinks)
      .join('text')
      .attr('font-size', 10)
      .attr('font-family', '"Rajdhani", SFMono-Regular, Menlo, Monaco, Consolas, monospace')
      .attr('letter-spacing', '0.05em')
      .attr('fill', cssVar('--color-zinc-600'))
      .attr('fill-opacity', 0)
      .attr('text-anchor', 'middle')
      .attr('dy', -4)
      .text(d => d.relation.replace(/_/g, ' ').toUpperCase());

    // Node groups
    const node = g.append('g')
      .selectAll<SVGGElement, GraphNode>('g')
      .data(graphNodes)
      .join('g')
      .style('cursor', 'pointer')
      .on('click', (_, d) => onSelectNode(d.id));

    // Selection ring (orange, behind the node)
    node.append('circle')
      .attr('r', 19)
      .attr('fill', d => d.id === selectedNodeId ? 'rgba(249,115,22,0.08)' : 'none') // orange-900/8
      .attr('stroke', '#ea580c') // orange-600
      .attr('stroke-width', 1.5)
      .attr('stroke-dasharray', '3 2')
      .attr('opacity', d => d.id === selectedNodeId ? 1 : 0)
      .attr('class', 'selection-ring');

    // Node outer ring (type accent, very subtle)
    node.append('circle')
      .attr('r', 14)
      .attr('fill', 'none')
      .attr('stroke', d => TYPE_ACCENT[d.type] || '#52525b')
      .attr('stroke-width', 1)
      .attr('stroke-opacity', 0.25);

    // Node main circle
    node.append('circle')
      .attr('r', 12)
      .attr('fill', d => {
        const c = d3.color(TYPE_ACCENT[d.type] || '#52525b');
        return c ? c.copy({ opacity: 0.12 }).formatRgb() : '#18181b';
      })
      .attr('stroke', cssVar('--color-zinc-700'))
      .attr('stroke-width', 1);

    // Inner dot (type accent, solid, tiny)
    node.append('circle')
      .attr('r', 3)
      .attr('fill', d => TYPE_ACCENT[d.type] || '#52525b')
      .attr('fill-opacity', 0.7);

    // Node labels: uppercase, monospace, tracking-widest
    node.append('text')
      .attr('dy', 26)
      .attr('text-anchor', 'middle')
      .attr('font-size', 11)
      .attr('font-family', '"Rajdhani", SFMono-Regular, Menlo, Monaco, Consolas, monospace')
      .attr('letter-spacing', '0.1em')
      .attr('fill', cssVar('--color-zinc-500'))
      .attr('pointer-events', 'none')
      .text(d => {
        const label = d.title.length > 18 ? d.title.slice(0, 16) + '..' : d.title;
        return label.toUpperCase();
      });

    // Tooltip: matches Panel component (border-zinc-800, bg-zinc-900)
    const tooltip = d3.select(container)
      .append('div')
      .attr('data-graph-tooltip', '')
      .style('position', 'absolute')
      .style('pointer-events', 'none')
      .style('opacity', '0')
      .style('background', cssVar('--color-zinc-900') + 'f2')
      .style('border', '1px solid ' + cssVar('--color-zinc-800'))
      .style('padding', '8px 12px')
      .style('font-family', '"Rajdhani", SFMono-Regular, Menlo, Monaco, Consolas, monospace')
      .style('font-size', '13px')
      .style('color', cssVar('--color-zinc-400'))
      .style('max-width', '240px')
      .style('z-index', '50')
      .style('transition', 'opacity 150ms');

    node.on('mouseenter', (event, d) => {
      // Highlight connected edges + show their labels
      link.attr('stroke-opacity', l => {
        const src = typeof l.source === 'object' ? (l.source as GraphNode).id : l.source;
        const tgt = typeof l.target === 'object' ? (l.target as GraphNode).id : l.target;
        return src === d.id || tgt === d.id ? 0.9 : 0.15;
      }).attr('stroke', l => {
        const src = typeof l.source === 'object' ? (l.source as GraphNode).id : l.source;
        const tgt = typeof l.target === 'object' ? (l.target as GraphNode).id : l.target;
        return src === d.id || tgt === d.id
          ? (TYPE_ACCENT[d.type] || '#71717a')
          : EDGE_COLOR;
      });
      edgeLabel.attr('fill-opacity', l => {
        const src = typeof l.source === 'object' ? (l.source as GraphNode).id : l.source;
        const tgt = typeof l.target === 'object' ? (l.target as GraphNode).id : l.target;
        return src === d.id || tgt === d.id ? 0.8 : 0;
      });

      // Brighten hovered node
      d3.select(event.currentTarget).selectAll('circle')
        .filter((_, i) => i === 2) // main circle
        .attr('fill', () => {
          const c = d3.color(TYPE_ACCENT[d.type] || '#52525b');
          return c ? c.copy({ opacity: 0.25 }).formatRgb() : '#27272a';
        })
        .attr('stroke', TYPE_ACCENT[d.type] || '#52525b')
        .attr('stroke-opacity', 0.5);

      const accent = TYPE_ACCENT[d.type] || '#71717a';
      const headColor = cssVar('--color-zinc-100');
      const summaryColor = cssVar('--color-zinc-500');
      tooltip
        .style('opacity', '1')
        .html(
          `<div style="color:${headColor};font-weight:700;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.1em;font-size:12px">${d.title}</div>` +
          `<div style="color:${accent};font-size:11px;text-transform:uppercase;letter-spacing:0.15em;margin-bottom:4px">${d.type.replace(/_/g, ' ')}</div>` +
          (d.summary ? `<div style="color:${summaryColor};font-size:12px;line-height:1.4">${d.summary}</div>` : '')
        );

      const rect = container.getBoundingClientRect();
      tooltip
        .style('left', (event.clientX - rect.left + 14) + 'px')
        .style('top', (event.clientY - rect.top - 14) + 'px');
    })
    .on('mousemove', (event) => {
      const rect = container.getBoundingClientRect();
      tooltip
        .style('left', (event.clientX - rect.left + 14) + 'px')
        .style('top', (event.clientY - rect.top - 14) + 'px');
    })
    .on('mouseleave', (event, d) => {
      link.attr('stroke-opacity', 0.6).attr('stroke', EDGE_COLOR);
      edgeLabel.attr('fill-opacity', 0);
      tooltip.style('opacity', '0');

      // Restore node
      d3.select(event.currentTarget).selectAll('circle')
        .filter((_, i) => i === 2)
        .attr('fill', () => {
          const c = d3.color(TYPE_ACCENT[d.type] || '#52525b');
          return c ? c.copy({ opacity: 0.12 }).formatRgb() : '#18181b';
        })
        .attr('stroke', cssVar('--color-zinc-700'))
        .attr('stroke-opacity', 1);
    });

    // Drag behavior
    const drag = d3.drag<SVGGElement, GraphNode>()
      .on('start', (event, d) => {
        if (!event.active) simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on('drag', (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on('end', (event, d) => {
        if (!event.active) simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
      });
    node.call(drag);

    // Force simulation
    const simulation = d3.forceSimulation(graphNodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(graphLinks).id(d => d.id).distance(140))
      .force('charge', d3.forceManyBody().strength(-350))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide(36));

    simulation.on('tick', () => {
      link
        .attr('x1', d => (d.source as GraphNode).x!)
        .attr('y1', d => (d.source as GraphNode).y!)
        .attr('x2', d => (d.target as GraphNode).x!)
        .attr('y2', d => (d.target as GraphNode).y!);

      edgeLabel
        .attr('x', d => ((d.source as GraphNode).x! + (d.target as GraphNode).x!) / 2)
        .attr('y', d => ((d.source as GraphNode).y! + (d.target as GraphNode).y!) / 2);

      node.attr('transform', d => `translate(${d.x},${d.y})`);
    });

    simulationRef.current = simulation;

    return () => { tooltip.remove(); };
  }, [nodes, edges, selectedNodeId, onSelectNode]);

  useEffect(() => {
    const cleanup = buildGraph();
    return () => {
      if (simulationRef.current) {
        simulationRef.current.stop();
        simulationRef.current = null;
      }
      if (cleanup) cleanup();
    };
  }, [buildGraph]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const observer = new ResizeObserver(() => buildGraph());
    observer.observe(container);
    return () => observer.disconnect();
  }, [buildGraph]);

  return (
    <div ref={containerRef} className="relative w-full h-full min-h-[400px]">
      <svg ref={svgRef} className="w-full h-full" />
      {nodes.length === 0 && (
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-600">
            No nodes in the knowledge graph yet
          </span>
        </div>
      )}
    </div>
  );
}
