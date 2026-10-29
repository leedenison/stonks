import type { Ticker } from "@/gen/admin/coverage/v1/coverage_pb";
import { TickerValidity } from "@/gen/admin/coverage/v1/coverage_pb";
import type { Period } from "@/gen/type/v1/type_pb";
import { bounds, cutDays, dayNumber } from "@/lib/coverage";

// The drawing's coordinates, in viewBox units; the gutter is the left margin
// the lane labels take.
const width = 1000;
const gutter = 120;
const laneHeight = 18;
const laneGap = 8;
const axisHeight = 28;

type Lane = { key: string; label: string; children: React.ReactNode };

// Timeline draws a ticker's facts and validity as lanes on one date axis. It
// draws SVG directly, since lanes are rectangles rather than series data;
// Recharts stays the library for charts of series.
export function Timeline({ ticker }: { ticker: Ticker }) {
  const b = bounds(ticker);
  if (!b) {
    return (
      <p data-testid="coverage-timeline" className="text-sm text-text-muted">
        Nothing to show.
      </p>
    );
  }
  const from = dayNumber(b.from);
  const span = Math.max(dayNumber(b.before) - from, 1);
  const x = (day: string) =>
    gutter + ((dayNumber(day) - from) / span) * (width - gutter);
  // An open bound runs to the drawing's edge.
  const bar = (p: Period | undefined) => {
    const x0 = p?.from === undefined ? gutter : x(p.from);
    const x1 = p?.before === undefined ? width : x(p.before);
    return { x: x0, width: Math.max(x1 - x0, 1) };
  };

  const datasources = [
    ...new Set(ticker.assertionCoverage.map((p) => p.datasource)),
  ];
  const lanes: Lane[] = [
    {
      key: "txns",
      label: "Transactions",
      children: ticker.txnDays.map((d) => (
        <rect
          key={d}
          x={x(d) - 0.5}
          y={3}
          width={1.5}
          height={laneHeight - 6}
          className="fill-text-muted"
        >
          <title>{`Transaction on ${d}`}</title>
        </rect>
      )),
    },
    {
      key: "validity",
      label: "Ticker validity",
      children: ticker.intervals.map((iv, i) => (
        <rect
          key={i}
          {...bar(iv.period)}
          y={2}
          height={laneHeight - 4}
          rx={2}
          data-validity={
            iv.validity === TickerValidity.CONFIRMED
              ? "confirmed"
              : "provisional"
          }
          className={
            iv.validity === TickerValidity.CONFIRMED
              ? "fill-primary"
              : "fill-primary/35"
          }
        >
          <title>{`${iv.validity === TickerValidity.CONFIRMED ? "Confirmed" : "Provisional"} ${iv.period?.from ?? ""} to ${iv.period?.before ?? ""}`}</title>
        </rect>
      )),
    },
    {
      key: "gaps",
      label: "Gaps",
      children: ticker.gaps.map((g) => (
        <rect
          key={g.period?.from}
          {...bar(g.period)}
          y={2}
          height={laneHeight - 4}
          rx={2}
          className="fill-accent/70"
        >
          <title>{`Gap ${g.period?.from ?? ""} to ${g.period?.before ?? ""}: ${g.txns} transactions of ${g.keys} keys`}</title>
        </rect>
      )),
    },
    {
      key: "assertions",
      label: "Assertions",
      children: ticker.assertions.map((a) => {
        const attested = a.source.case === "attestedBy";
        // Tailwind generates only class names spelled out in full.
        const fill = attested ? "fill-accent" : "fill-primary";
        const ring = attested ? "stroke-accent" : "stroke-primary";
        return (
          <rect
            key={a.id}
            {...bar(a.period)}
            y={laneHeight / 2 - 5}
            height={10}
            rx={2}
            data-attested={attested ? "true" : undefined}
            data-negative={a.instrId === undefined ? "true" : undefined}
            className={
              a.instrId === undefined
                ? `fill-surface ${ring} stroke-2`
                : `${fill} opacity-70`
            }
          >
            <title>{`${a.source.value ?? ""} asserted ${a.instrId ?? "nothing"} from ${a.period?.from ?? ""} up to ${a.period?.before ?? ""}`}</title>
          </rect>
        );
      }),
    },
    {
      key: "edges",
      label: "Edges",
      children: ticker.assertions.flatMap((a) =>
        cutDays(a).map((cut) => {
          const cx = x(cut);
          return (
            <polygon
              key={`${a.id}-${cut}`}
              points={`${cx},2 ${cx + 6},${laneHeight - 2} ${cx - 6},${laneHeight - 2}`}
              data-cut={cut}
              className="fill-text-muted"
            >
              <title>{`${a.source.value ?? ""}: ${ticker.ticker?.value ?? ""}: a change at the start of ${cut}`}</title>
            </polygon>
          );
        }),
      ),
    },
    ...datasources.map((ds) => ({
      key: `coverage-${ds}`,
      label: ds,
      children: ticker.assertionCoverage
        .filter((p) => p.datasource === ds)
        .map((p, i) => (
          <rect
            key={i}
            {...bar(p.period)}
            y={4}
            height={laneHeight - 8}
            rx={2}
            className="fill-primary/55"
          >
            <title>{`${ds} reported every edge of ${ticker.ticker?.value ?? ""} ${p.period?.from ?? ""} to ${p.period?.before ?? ""}`}</title>
          </rect>
        )),
    })),
  ];
  const height = lanes.length * (laneHeight + laneGap) + axisHeight;

  return (
    <svg
      data-testid="coverage-timeline"
      viewBox={`0 0 ${width} ${height}`}
      className="w-full text-xs"
      role="img"
      aria-label={`Timeline of ${ticker.ticker?.value ?? ""}`}
    >
      {lanes.map((lane, i) => {
        const y = i * (laneHeight + laneGap);
        return (
          <g
            key={lane.key}
            data-lane={lane.key}
            transform={`translate(0 ${y})`}
          >
            <text
              x={0}
              y={laneHeight / 2}
              dominantBaseline="middle"
              className="fill-text-muted font-sans"
            >
              {lane.label}
            </text>
            <line
              x1={gutter}
              x2={width}
              y1={laneHeight / 2}
              y2={laneHeight / 2}
              className="stroke-border"
            />
            {lane.children}
          </g>
        );
      })}
      <g
        data-lane="axis"
        transform={`translate(0 ${lanes.length * (laneHeight + laneGap)})`}
      >
        <line x1={gutter} x2={width} y1={0} y2={0} className="stroke-border" />
        {ticks(b.from, b.before).map((t) => (
          <g key={t.day} transform={`translate(${x(t.day)} 0)`}>
            <line y1={0} y2={5} className="stroke-text-muted" />
            <text
              y={18}
              textAnchor="middle"
              className="fill-text-muted font-mono tabular-nums"
            >
              {t.label}
            </text>
          </g>
        ))}
      </g>
    </svg>
  );
}

// ticks returns the axis labels of a range: by year from two years up, and
// by month below.
export function ticks(
  from: string,
  before: string,
): { day: string; label: string }[] {
  const [y0, m0] = from.split("-").map(Number);
  const [y1, m1] = before.split("-").map(Number);
  const months = (y1 - y0) * 12 + (m1 - m0);
  const out: { day: string; label: string }[] = [];
  if (months >= 24) {
    for (let y = y0 + 1; y <= y1; y++) {
      out.push({ day: `${y}-01-01`, label: String(y) });
    }
    return out;
  }
  let y = y0;
  let m = m0 + 1;
  while (y * 12 + m <= y1 * 12 + m1) {
    if (m > 12) {
      y += 1;
      m = 1;
    }
    const mm = String(m).padStart(2, "0");
    out.push({ day: `${y}-${mm}-01`, label: `${y}-${mm}` });
    m += 1;
  }
  return out;
}
