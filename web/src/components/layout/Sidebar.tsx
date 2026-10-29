import { NavLink } from 'react-router-dom';
import './Sidebar.css';

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/providers', label: 'Providers' },
  { to: '/projects', label: 'Projects' },
  { to: '/requests', label: 'Requests' },
];

export function Sidebar() {
  return (
    <nav className="sidebar">
      <div className="sidebar__brand">
        <svg width="20" height="20" viewBox="0 0 32 32" fill="none" aria-hidden="true">
          <path
            d="M8 20 L13 11 L18 17 L24 9"
            stroke="var(--accent)"
            strokeWidth="3"
            strokeLinecap="round"
            strokeLinejoin="round"
            fill="none"
          />
          <circle cx="24" cy="9" r="2.2" fill="var(--accent)" />
        </svg>
        <span>Gateway Console</span>
      </div>
      <ul className="sidebar__nav">
        {NAV_ITEMS.map((item) => (
          <li key={item.to}>
            <NavLink
              to={item.to}
              end={item.end}
              className={({ isActive }) => `sidebar__link${isActive ? ' sidebar__link--active' : ''}`}
            >
              {item.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
