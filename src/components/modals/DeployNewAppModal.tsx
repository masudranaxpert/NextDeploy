import React, { useState } from 'react';
import { Layers, GitBranch, FileCode2, Upload, Plus } from 'lucide-react';
import { ShadcnDialog } from '../ui/Primitives';

export interface DeployNewAppModalProps {
  open: boolean;
  onClose: () => void;
  onCreateApp: (newApp: {
    name: string;
    description: string;
    sourceType: 'git' | 'compose' | 'zip';
    gitRepo?: string;
    gitBranch?: string;
    primaryDomain?: string;
    composeContent: string;
  }) => void;
}

export const DeployNewAppModal: React.FC<DeployNewAppModalProps> = ({
  open,
  onClose,
  onCreateApp,
}) => {
  const [name, setName] = useState('');
  const [sourceType, setSourceType] = useState<'git' | 'compose' | 'zip'>('git');
  const [gitRepo, setGitRepo] = useState('');
  const [gitBranch, setGitBranch] = useState('main');
  const [composeContent, setComposeContent] = useState(
    `services:\n  app:\n    build: .\n    restart: unless-stopped\n    ports:\n      - "3000:3000"\n    environment:\n      - NODE_ENV=production`
  );

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    onCreateApp({
      name: name.trim().toLowerCase().replace(/[^a-z0-9-]/g, '-'),
      description: '',
      sourceType,
      gitRepo: sourceType === 'git' ? gitRepo.trim() : undefined,
      gitBranch: sourceType === 'git' ? gitBranch.trim() : undefined,
      composeContent: sourceType === 'compose' ? composeContent : '',
    });
    setName('');
    setGitRepo('');
    onClose();
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Create Project"
      icon={<Layers className="h-4 w-4" />}
      maxWidth="lg"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {/* Source Type Cards (Dokploy style) */}
        <div className="grid grid-cols-3 gap-2">
          {(
            [
              { id: 'git', label: 'Git Repository', sub: 'GitHub / GitLab', icon: <GitBranch className="h-4 w-4" /> },
              { id: 'compose', label: 'Docker Compose', sub: 'Raw YAML', icon: <FileCode2 className="h-4 w-4" /> },
              { id: 'zip', label: 'Archive Upload', sub: '.zip / .tar.gz', icon: <Upload className="h-4 w-4" /> },
            ] as const
          ).map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setSourceType(tab.id)}
              className={`flex flex-col items-start gap-1 rounded-xl border p-3 text-left transition-all cursor-pointer ${
                sourceType === tab.id
                  ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100 ring-1 ring-orange-500/20'
                  : 'border-white/[0.07] bg-zinc-900/50 text-zinc-400 hover:border-white/[0.15] hover:text-zinc-200'
              }`}
            >
              <span className={sourceType === tab.id ? 'text-orange-400' : 'text-zinc-400'}>
                {tab.icon}
              </span>
              <span className="text-xs font-semibold">{tab.label}</span>
              <span className="text-[10px] text-zinc-500">{tab.sub}</span>
            </button>
          ))}
        </div>

        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            Project Slug
          </label>
          <input
            type="text"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="my-app"
            className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-600 focus:border-orange-500 focus:outline-none"
          />
        </div>

        {sourceType === 'git' && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="sm:col-span-2">
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Git Repository URL
              </label>
              <input
                type="text"
                required
                value={gitRepo}
                onChange={(e) => setGitRepo(e.target.value)}
                placeholder="https://github.com/org/repo"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-600 focus:border-orange-500 focus:outline-none"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Branch</label>
              <input
                type="text"
                value={gitBranch}
                onChange={(e) => setGitBranch(e.target.value)}
                placeholder="main"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-orange-400 font-semibold placeholder-zinc-600 focus:border-orange-500 focus:outline-none"
              />
            </div>
          </div>
        )}

        {sourceType === 'compose' && (
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              docker-compose.yml
            </label>
            <textarea
              rows={5}
              value={composeContent}
              onChange={(e) => setComposeContent(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] p-3 font-mono text-xs text-zinc-200 focus:border-orange-500 focus:outline-none"
            />
          </div>
        )}

        {sourceType === 'zip' && (
          <div className="rounded-xl border border-dashed border-white/[0.1] bg-zinc-900/30 p-6 text-center">
            <Upload className="mx-auto h-7 w-7 text-zinc-500" />
            <div className="mt-2 text-xs font-medium text-zinc-300">
              Upload .zip or .tar.gz archive
            </div>
            <p className="mt-1 text-[11px] text-zinc-500">
              You can upload your archive inside the project workspace files manager after creating.
            </p>
          </div>
        )}

        <div className="flex items-center justify-end gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 active:scale-[0.98] transition-all duration-100 ease-out cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs active:scale-[0.98] transition-all duration-100 ease-out cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Create & Deploy
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
