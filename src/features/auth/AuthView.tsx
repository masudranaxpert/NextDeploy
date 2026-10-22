import React, { useState } from 'react';
import { ShieldCheck, Lock, User, ArrowRight, Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';

interface AuthViewProps {
  mode: 'setup' | 'login';
  onSuccess: () => void;
}

export const AuthView: React.FC<AuthViewProps> = ({ mode, onSuccess }) => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);

    try {
      if (mode === 'setup') {
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
    <div className="flex min-h-screen items-center justify-center bg-[#09090b] px-4 py-12 text-zinc-100">
      <div className="w-full max-w-sm space-y-6">
        <div className="text-center space-y-2">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl border border-orange-500/20 bg-orange-500/10 shadow-lg shadow-orange-500/10">
            <span className="font-mono text-xl font-black text-orange-500">nd</span>
          </div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100">
            {mode === 'setup' ? 'Welcome to NextDeploy' : 'Sign in to NextDeploy'}
          </h1>
          <p className="text-xs text-zinc-400">
            {mode === 'setup'
              ? 'Create your primary administrator account to get started'
              : 'Enter your credentials to access the control plane'}
          </p>
        </div>

        <div className="rounded-2xl border border-white/[0.08] bg-[#111115] p-6 shadow-2xl">
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                Username
              </label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
                <input
                  type="text"
                  required
                  autoFocus
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="admin"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] pl-9 pr-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none"
                />
              </div>
            </div>

            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                Password
              </label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
                <input
                  type="password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••••••"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] pl-9 pr-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none"
                />
              </div>
            </div>

            {mode === 'setup' && (
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                  Confirm Password
                </label>
                <div className="relative">
                  <ShieldCheck className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
                  <input
                    type="password"
                    required
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    placeholder="••••••••••••"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] pl-9 pr-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none"
                  />
                </div>
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="mt-2 flex w-full items-center justify-center gap-2 rounded-lg bg-orange-600 px-4 py-2.5 text-xs font-semibold text-white transition hover:bg-orange-500 disabled:opacity-50 cursor-pointer shadow-lg shadow-orange-600/20"
            >
              {loading ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <>
                  <span>{mode === 'setup' ? 'Create Admin Account' : 'Sign In'}</span>
                  <ArrowRight className="h-3.5 w-3.5" />
                </>
              )}
            </button>
          </form>
        </div>

        <div className="text-center font-mono text-[11px] text-zinc-500">
          NextDeploy v2.4 · Docker Compose PaaS
        </div>
      </div>
    </div>
  );
};
