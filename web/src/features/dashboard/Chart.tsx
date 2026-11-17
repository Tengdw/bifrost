import { useEffect, useRef } from "react";
import { init, use } from "echarts/core";
import type { ComposeOption, EChartsType } from "echarts/core";
import { LineChart, PieChart } from "echarts/charts";
import type { LineSeriesOption, PieSeriesOption } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
import type {
  GridComponentOption,
  TooltipComponentOption,
} from "echarts/components";
import { SVGRenderer } from "echarts/renderers";

use([LineChart, PieChart, GridComponent, TooltipComponent, SVGRenderer]);

export type ChartOption = ComposeOption<
  | LineSeriesOption
  | PieSeriesOption
  | GridComponentOption
  | TooltipComponentOption
>;

export function Chart({
  option,
  label,
}: {
  option: ChartOption;
  label: string;
}) {
  const element = useRef<HTMLDivElement>(null);
  const chart = useRef<EChartsType>(null);

  useEffect(() => {
    const target = element.current;
    if (!target) return;
    const instance = init(target, undefined, { renderer: "svg" });
    chart.current = instance;
    const observer = new ResizeObserver(() => instance.resize());
    observer.observe(target);
    return () => {
      observer.disconnect();
      instance.dispose();
      chart.current = null;
    };
  }, []);

  useEffect(() => {
    chart.current?.setOption({ animation: false, ...option }, true);
  }, [option]);

  return <div ref={element} className="chart" role="img" aria-label={label} />;
}
