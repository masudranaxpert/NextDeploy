import React from 'react';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import type { TelemetryPoint } from '../../types/nextdeploy';

export interface EvilAreaChartProps {
  data: TelemetryPoint[];
  dataKey: keyof TelemetryPoint;
  secondaryKey?: keyof TelemetryPoint;
  color?: string;
  secondaryColor?: string;
  unit?: string;
  height?: number;
  label?: string;
  secondaryLabel?: string;
}

export const EvilAreaChart: React.FC<EvilAreaChartProps> = ({
  data,
  dataKey,
  secondaryKey,
  color = '#f97316',
  secondaryColor = '#38bdf8',
  unit = '%',
  height = 190,
  label,
  secondaryLabel,
}) => {
  const gradId1 = `evil-grad-${String(dataKey)}`;
  const gradId2 = `evil-grad-${String(secondaryKey || 'sec')}`;

  return (
    <div style={{ height, minHeight: height }} className="w-full min-w-0 select-none">
      <ResponsiveContainer width="100%" height={height} minWidth={0} initialDimension={{ width: 640, height }}>
        <AreaChart data={data} margin={{ top: 8, right: 6, left: -22, bottom: 0 }}>
          <defs>
            <linearGradient id={gradId1} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.32} />
              <stop offset="85%" stopColor={color} stopOpacity={0.0} />
            </linearGradient>
            {secondaryKey && (
              <linearGradient id={gradId2} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={secondaryColor} stopOpacity={0.24} />
                <stop offset="85%" stopColor={secondaryColor} stopOpacity={0.0} />
              </linearGradient>
            )}
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
          <XAxis
            dataKey="time"
            stroke="#71717a"
            fontSize={10}
            tickLine={false}
            axisLine={false}
            fontFamily="JetBrains Mono"
          />
          <YAxis
            stroke="#71717a"
            fontSize={10}
            tickLine={false}
            axisLine={false}
            fontFamily="JetBrains Mono"
          />
          <Tooltip
            contentStyle={{
              backgroundColor: '#0e0e12',
              borderColor: 'rgba(255,255,255,0.09)',
              borderRadius: '10px',
              fontSize: '11px',
              fontFamily: 'JetBrains Mono',
              color: '#fafafa',
              boxShadow: '0 12px 32px rgba(0,0,0,0.65)',
            }}
            formatter={(val, name) => [
              `${typeof val === 'number' ? Number(val).toFixed(1) : (val ?? 0)}${unit}`,
              name === dataKey ? label || String(dataKey) : secondaryLabel || String(secondaryKey),
            ]}
          />
          <Area
            type="monotone"
            dataKey={dataKey}
            stroke={color}
            strokeWidth={2}
            fillOpacity={1}
            fill={`url(#${gradId1})`}
            isAnimationActive={false}
          />
          {secondaryKey && (
            <Area
              type="monotone"
              dataKey={secondaryKey}
              stroke={secondaryColor}
              strokeWidth={1.75}
              fillOpacity={1}
              fill={`url(#${gradId2})`}
              isAnimationActive={false}
            />
          )}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
};

export default EvilAreaChart;
