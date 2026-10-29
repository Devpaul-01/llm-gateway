import type { InputHTMLAttributes } from 'react';
import './Input.css';

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`input${props.className ? ` ${props.className}` : ''}`} />;
}
