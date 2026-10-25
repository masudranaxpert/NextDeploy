import React, { useEffect, useState } from 'react';
import { ArrowRight, Eye, EyeOff, Loader2, Lock, ShieldCheck, User } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';

interface AuthViewProps {
  mode: 'setup' | 'login';
  onSuccess: () => void;
}

const NextDeployMark: React.FC<{ className?: string }> = ({ className }) => (
  <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
    <defs>
      <linearGradient id="nd-mark" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#f99c00" />
        <stop offset="1" stopColor="#fe6e00" />
      </linearGradient>
    </defs>
    <rect width="64" height="64" rx="15" fill="url(#nd-mark)" />
    <g fill="none" stroke="#fff" strokeWidth="6.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M16 45V20l24 24V19" />
      <path d="M32 26l8-8 8 8" />
    </g>
  </svg>
);

const STRENGTH = [
  { label: 'Weak', color: '#ff2357' },
  { label: 'Fair', color: '#fe6e00' },
  { label: 'Good', color: '#f99c00' },
  { label: 'Strong', color: '#00bb7f' },
];

function passwordScore(v: string): number {
  if (!v) return 0;
  let s = 0;
  if (v.length >= 8) s++;
  if (v.length >= 12) s++;
  if (/[A-Z]/.test(v) && /[a-z]/.test(v)) s++;
  if (/\d/.test(v) && /[^A-Za-z0-9]/.test(v)) s++;
  return Math.max(1, s);
}

const DEPLOY_LOG: React.ReactNode[] = [
  <><span className="text-zinc-500">$</span> nd deploy <span className="text-[#f99c00]">api-gateway</span></>,
  <><span className="text-[#8d54ff]">→</span> pulling git@github.com:acme/api <span className="text-zinc-500">main@4f2c9e1</span></>,
  <><span className="text-[#8d54ff]">→</span> building image <span className="text-zinc-500">(cached 7/9 layers)</span></>,
  <><span className="text-[#8d54ff]">→</span> compose up · 3 services</>,
  <><span className="text-[#00bb7f]">✓</span> health check passed <span className="text-zinc-500">200 OK · 38ms</span></>,
  <><span className="text-[#00bb7f]">✓</span> TLS issued for <span className="text-[#f99c00]">api.acme.dev</span></>,
  <><span className="text-[#00bb7f]">✓</span> deployed in <b>11.8s</b></>,
];

// Replays the sample deploy log line by line, then starts over.
const DeployLog: React.FC = () => {
  const [shown, setShown] = useState(0);

  useEffect(() => {
    const delay = shown < DEPLOY_LOG.length ? (shown === 0 ? 350 : 650) : 3200;
    const t = setTimeout(() => setShown((n) => (n < DEPLOY_LOG.length ? n + 1 : 0)), delay);
    return () => clearTimeout(t);
  }, [shown]);

  return (
    <div className="min-h-[214px] px-[18px] py-4 font-mono text-[12.5px] leading-[1.85] text-zinc-300">
      {DEPLOY_LOG.slice(0, shown).map((line, i) => (
        <div key={i} className="nd-log-line truncate">{line}</div>
      ))}
    </div>
  );
};

interface PasswordFieldProps {
  id: string;
  label: string;
  icon: React.ReactNode;
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  autoComplete: string;
  children?: React.ReactNode;
}

const inputClass =
  'h-11 w-full rounded-[10px] border border-white/[0.12] bg-[#0b0b0e] pl-10 text-sm text-zinc-100 placeholder-zinc-600 outline-none transition hover:border-white/[0.18] focus:border-[#fe6e00]/70 focus:bg-[#0d0d10] focus:ring-4 focus:ring-[#fe6e00]/[0.12]';

const iconClass =
  'pointer-events-none absolute left-[13px] top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500 transition group-focus-within:text-[#f99c00]';

const PasswordField: React.FC<PasswordFieldProps> = ({ id, label, icon, value, onChange, placeholder, autoComplete, children }) => {
  const [visible, setVisible] = useState(false);
  return (
    <div>
      <label htmlFor={id} className="mb-[7px] block text-[13px] font-medium text-zinc-300">
        {label}
      </label>
      <div className="group relative">
        <span className={iconClass}>{icon}</span>
        <input
          id={id}
          type={visible ? 'text' : 'password'}
          required
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          autoComplete={autoComplete}
          className={`${inputClass} pr-11`}
        />
        <button
          type="button"
          onClick={() => setVisible((v) => !v)}
          aria-label={visible ? 'Hide password' : 'Show password'}
          className="absolute right-1.5 top-1/2 grid h-8 w-8 -translate-y-1/2 cursor-pointer place-items-center rounded-lg text-zinc-500 transition hover:bg-white/5 hover:text-zinc-100"
        >
          {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
        </button>
      </div>
      {children}
    </div>
  );
};

export const AuthView: React.FC<AuthViewProps> = ({ mode, onSuccess }) => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [loading, setLoading] = useState(false);

  const isSetup = mode === 'setup';
  const score = passwordScore(password);
  const strength = score ? STRENGTH[score - 1] : null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);

    try {
      if (isSetup) {
        if (password !== confirm) {
          toast.error('Passwords do not match');
          setLoading(false);
          return;
        }
        const res = await backendApi.setup(username.trim(), password, confirm);
        if (res.ok) {
          toast.success('Admin account created successfully');
          onSuccess();
        } else {
          toast.error(res.error || 'Failed to complete setup');
        }
      } else {
        const res = await backendApi.login(username.trim(), password);
        if (res.ok) {
          toast.success('Logged in successfully');
          onSuccess();
        } else {
          toast.error(res.error || 'Invalid credentials');
        }
      }
    } catch {
      toast.error('Network request failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="grid min-h-screen bg-[#09090b] text-zinc-100 min-[900px]:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      <section className="flex flex-col px-4 pb-6 pt-8 min-[900px]:px-12 min-[900px]:py-8">
        <div className="flex items-center gap-2.5 font-display text-[17px] font-bold tracking-tight">
          <NextDeployMark className="h-[30px] w-[30px]" />
          <div>
            Next<span className="text-[#fe6e00]">Deploy</span>
          </div>
        </div>

        <div className="flex flex-1 items-center justify-center py-10">
          <form onSubmit={handleSubmit} className="w-full max-w-[380px]">
            {isSetup && (
              <div className="mb-2.5 font-mono text-[11px] font-medium uppercase tracking-[0.12em] text-[#f99c00]">
                First-time setup
              </div>
            )}
            <h1 className="mb-2 text-[28px] font-bold leading-tight tracking-tight">
              {isSetup ? 'Create admin account' : 'Welcome back'}
            </h1>
            <p className="mb-7 text-sm text-zinc-400">
              {isSetup ? 'This account will have full access to the panel.' : 'Sign in to manage your servers and deployments.'}
            </p>

            <div className="space-y-4">
              <div>
                <label htmlFor="nd-username" className="mb-[7px] block text-[13px] font-medium text-zinc-300">
                  Username
                </label>
                <div className="group relative">
                  <User className={iconClass} />
                  <input
                    id="nd-username"
                    type="text"
                    required
                    autoFocus
                    autoComplete="username"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    placeholder="admin"
                    className={`${inputClass} pr-3`}
                  />
                </div>
              </div>

              <PasswordField
                id="nd-password"
                label="Password"
                icon={<Lock className="h-4 w-4" />}
                value={password}
                onChange={setPassword}
                placeholder={isSetup ? 'At least 12 characters' : 'Enter your password'}
                autoComplete={isSetup ? 'new-password' : 'current-password'}
              >
                {isSetup && (
                  <>
                    <div className="mt-[9px] flex gap-1">
                      {STRENGTH.map((_, i) => (
                        <i
                          key={i}
                          className="h-[3px] flex-1 rounded-full bg-white/[0.12] transition-colors"
                          style={i < score && strength ? { background: strength.color } : undefined}
                        />
                      ))}
                    </div>
                    <div className="mt-1.5 flex justify-between text-xs text-zinc-500">
                      <span style={strength ? { color: strength.color } : undefined}>
                        {strength ? `${strength.label} password` : 'Use 12+ characters with a mix of types'}
                      </span>
                      <span className="font-mono">{Math.min(password.length, 99)} / 12</span>
                    </div>
                  </>
                )}
              </PasswordField>

              {isSetup && (
                <PasswordField
                  id="nd-confirm"
                  label="Confirm password"
                  icon={<ShieldCheck className="h-4 w-4" />}
                  value={confirm}
                  onChange={setConfirm}
                  placeholder="Repeat password"
                  autoComplete="new-password"
                >
                  <div className={`mt-1.5 min-h-[18px] text-xs ${confirm === password ? 'text-[#00bb7f]' : 'text-[#ff2357]'}`}>
                    {confirm && (confirm === password ? '✓ Passwords match' : '✕ Passwords do not match')}
                  </div>
                </PasswordField>
              )}
            </div>

            <button
              type="submit"
              disabled={loading}
              className="group mt-6 flex h-[46px] w-full cursor-pointer items-center justify-center gap-2 rounded-[10px] bg-gradient-to-b from-[#ff8a1f] to-[#fe6e00] text-sm font-semibold text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.25),0_8px_24px_-8px_rgba(254,110,0,0.6)] transition hover:brightness-110 active:translate-y-px disabled:cursor-not-allowed disabled:opacity-60"
            >
              {loading ? (
                <Loader2 className="h-[18px] w-[18px] animate-spin" />
              ) : (
                <>
                  <span>{isSetup ? 'Create admin account' : 'Sign in'}</span>
                  <ArrowRight className="h-4 w-4 transition-transform group-hover:translate-x-[3px]" />
                </>
              )}
            </button>
          </form>
        </div>

        <div className="flex justify-between font-mono text-xs text-zinc-600">
          <span>v2.5</span>
          <span>Docker Compose PaaS</span>
        </div>
      </section>

      <aside
        aria-hidden="true"
        className="relative m-3 hidden flex-col justify-center overflow-hidden rounded-[20px] border border-white/[0.08] bg-[#0e0e11] p-14 min-[900px]:flex"
        style={{
          backgroundImage:
            'radial-gradient(120% 80% at 100% 0%, rgba(254,110,0,.22), transparent 55%), radial-gradient(90% 70% at 0% 100%, rgba(249,156,0,.10), transparent 60%)',
        }}
      >
        <div
          className="pointer-events-none absolute inset-0 opacity-60"
          style={{
            backgroundImage:
              'linear-gradient(rgba(255,255,255,.08) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.08) 1px, transparent 1px)',
            backgroundSize: '44px 44px',
            maskImage: 'radial-gradient(ellipse at 70% 30%, #000 10%, transparent 70%)',
            WebkitMaskImage: 'radial-gradient(ellipse at 70% 30%, #000 10%, transparent 70%)',
          }}
        />
        <div className="relative">
          <h2 className="mb-3.5 max-w-[460px] text-[34px] font-bold leading-[1.15] tracking-[-0.035em]">
            Push to deploy.
            <br />
            <span className="bg-gradient-to-r from-[#f99c00] to-[#fe6e00] bg-clip-text text-transparent">
              Your servers, your rules.
            </span>
          </h2>
          <p className="mb-9 max-w-[440px] text-[15px] text-zinc-400">
            Compose stacks, automatic HTTPS, PHP sites and live terminals, all from one self-hosted control plane.
          </p>

          <div className="max-w-[520px] overflow-hidden rounded-[14px] border border-white/[0.12] bg-[#09090b]/85 shadow-[0_30px_80px_-20px_rgba(0,0,0,0.8)] backdrop-blur">
            <div className="flex items-center gap-[7px] border-b border-white/[0.08] px-3.5 py-[11px]">
              <i className="h-2.5 w-2.5 rounded-full bg-zinc-800" />
              <i className="h-2.5 w-2.5 rounded-full bg-zinc-800" />
              <i className="h-2.5 w-2.5 rounded-full bg-zinc-800" />
              <b className="ml-2 font-mono text-xs font-medium text-zinc-500">deploy · api-gateway</b>
              <span className="ml-auto flex items-center gap-1.5 font-mono text-[11px] font-medium text-[#00bb7f]">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#00bb7f] shadow-[0_0_0_3px_rgba(0,187,127,0.2)]" />
                live
              </span>
            </div>
            <DeployLog />
          </div>

          <div className="mt-9 flex gap-8">
            {[
              ['Compose', 'multi-service stacks'],
              ['Auto TLS', 'via Caddy'],
              ['PHP & WP', 'built-in hosting'],
            ].map(([title, sub]) => (
              <div key={title}>
                <b className="block font-display text-[22px] font-bold tracking-tight">{title}</b>
                <span className="text-xs text-zinc-500">{sub}</span>
              </div>
            ))}
          </div>
        </div>
      </aside>
    </div>
  );
};
