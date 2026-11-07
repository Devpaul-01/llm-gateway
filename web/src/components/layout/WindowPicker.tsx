import { WINDOWS, type Window } from '../../api/types';
import './WindowPicker.css';

const LABELS: Record<Window, string> = {
  '1h': '1h',
  '6h': '6h',
  '24h': '24h',
  '7d': '7d',
  // Not shown here (WINDOWS omits it -- see api/types.ts), only used
  // directly by the public Demo page. Still required because LABELS is
  // keyed by the full Window type.
  all: 'All time',
};

interface WindowPickerProps {
  value: Window;
  onChange: (window: Window) => void;
}

export function WindowPicker({ value, onChange }: WindowPickerProps) {
  return (
    <div className="window-picker" role="group" aria-label="Time window">
      {WINDOWS.map((w) => (
        <button
          key={w}
          type="button"
          className={`window-picker__item${w === value ? ' window-picker__item--active' : ''}`}
          onClick={() => onChange(w)}
          aria-pressed={w === value}
        >
          {LABELS[w]}
        </button>
      ))}
    </div>
  );
}
