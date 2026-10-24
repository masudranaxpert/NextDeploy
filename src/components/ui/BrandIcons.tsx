import React from 'react';

interface IconProps extends React.SVGProps<SVGSVGElement> {
  className?: string;
  size?: number | string;
}

/** Official WordPress Logo */
export const WordPressIcon: React.FC<IconProps> = ({ className = 'h-4 w-4', size, ...props }) => (
  <svg
    viewBox="0 0 24 24"
    fill="currentColor"
    width={size}
    height={size}
    className={className}
    {...props}
  >
    <path d="M12 2C6.477 2 2 6.477 2 12c0 4.418 2.865 8.167 6.84 9.508L4.62 9.878a9.98 9.98 0 0 1 7.38-4.878zm6.54 13.978c.574-.972.86-2.072.86-3.238 0-1.874-.682-3.178-1.27-4.204-.775-1.31-1.503-2.378-1.503-3.66 0-1.433 1.086-2.756 2.62-2.756.07 0 .14.004.209.01A9.97 9.97 0 0 1 22 12c0 3.328-1.625 6.276-4.12 8.082l.66-4.104zM12 22a9.96 9.96 0 0 1-4.22-.934l4.28-12.44 4.38 12.01A9.94 9.94 0 0 1 12 22zM2.84 8.78l5.88 16.09A9.97 9.97 0 0 1 2 12c0-1.16.202-2.274.57-3.31l.27.09z" />
  </svg>
);

/** Official Laravel Logo */
export const LaravelIcon: React.FC<IconProps> = ({ className = 'h-4 w-4', size, ...props }) => (
  <svg
    viewBox="0 0 24 24"
    fill="currentColor"
    width={size}
    height={size}
    className={className}
    {...props}
  >
    <path d="M21.578 7.37L12.44.207a.93.93 0 0 0-.88 0L2.422 7.37a.936.936 0 0 0-.422.793v12.003c0 .324.168.623.44.787l9.138 5.485a.93.93 0 0 0 .964 0l9.036-5.485a.936.936 0 0 0 .422-.787V8.163a.936.936 0 0 0-.422-.793zm-9.578-5.34l7.65 6.002-3.418 2.052-7.65-6.002 3.418-2.052zm-8 7.558l7.142 5.603-3.19 1.916L3.98 11.51l.02-1.922zm8 12.553l-7.142-4.287v-5.466l7.142 5.603v4.15zm8.142-4.287l-7.142 4.287v-4.15l7.142-5.603v5.466zm-.02-7.234l-3.978 2.387-3.19-1.916 7.148-5.603.02 5.132z" />
  </svg>
);

/** Official PHP Logo */
export const PhpIcon: React.FC<IconProps> = ({ className = 'h-4 w-4', size, ...props }) => (
  <svg
    viewBox="0 0 24 24"
    fill="currentColor"
    width={size}
    height={size}
    className={className}
    {...props}
  >
    <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-5.25 13.5H5.2l1.6-7h2.2c1.2 0 1.95.6 1.7 1.8-.25 1.15-1.2 2-2.4 2h-.85l-.7 3.2zm6.75-3.2h-.95l-.7 3.2h-1.55l1.6-7h1.55l-.5 2.2h.95c1.2 0 1.95.6 1.7 1.8-.25 1.15-1.2 2-2.4 2v-.2zm5.7 3.2h-1.55l.7-3.2h-.85c-1.2 0-2.15-.85-1.9-2 .25-1.2 1.2-1.8 2.4-1.8h2.2l-1.6 7h.6-.8zm-11-5.1h.7c.6 0 1-.35 1.1-.9.1-.55-.2-.9-.8-.9h-.6l-.4 1.8zm11 0h.7c.6 0 1-.35 1.1-.9.1-.55-.2-.9-.8-.9h-.6l-.4 1.8z" />
  </svg>
);

/** Modern Static Web / HTML5 / Assets Icon */
export const StaticSiteIcon: React.FC<IconProps> = ({ className = 'h-4 w-4', size, ...props }) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    width={size}
    height={size}
    className={className}
    {...props}
  >
    <circle cx="12" cy="12" r="10" />
    <path d="m4.93 4.93 4.24 4.24" />
    <path d="m14.83 9.17 4.24-4.24" />
    <path d="m14.83 14.83 4.24 4.24" />
    <path d="m9.17 14.83-4.24 4.24" />
    <circle cx="12" cy="12" r="4" />
  </svg>
);

interface SiteTypeIconProps {
  type: string;
  name?: string;
  className?: string;
}

export const SiteTypeIcon: React.FC<SiteTypeIconProps> = ({ type, name = '', className = 'h-4 w-4' }) => {
  const isWp = type === 'wordpress' || name.toLowerCase().includes('wordpress') || name.toLowerCase().includes('-wp');
  const isLaravel = name.toLowerCase().includes('laravel') || name.toLowerCase().includes('crm') || name.toLowerCase().includes('artisan');
  const isStatic = type === 'static';

  if (isWp) {
    return <WordPressIcon className={`${className} text-[#3858E9]`} />;
  }
  if (isLaravel) {
    return <LaravelIcon className={`${className} text-[#FF2D20]`} />;
  }
  if (isStatic) {
    return <StaticSiteIcon className={`${className} text-sky-400`} />;
  }
  return <PhpIcon className={`${className} text-[#777BB4]`} />;
};
