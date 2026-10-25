import React, { useState } from 'react';
import { BookOpen, Check, Cpu, Database, HardDrive, Info, Layers, ShieldCheck, Zap } from 'lucide-react';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { MariaDbIcon } from '../../components/ui/BrandIcons';
import type { PhpInstanceData } from '../../lib/api/phpApi';

interface MariaDbTuningDocsModalProps {
  open: boolean;
  onClose: () => void;
  mysql: PhpInstanceData['mysql'];
}

export const MariaDbTuningDocsModal: React.FC<MariaDbTuningDocsModalProps> = ({
  open,
  onClose,
  mysql,
}) => {
  const [lang, setLang] = useState<'bn' | 'en'>('bn');

  const hostMB = mysql.host_memory_mb || 0;
  const memMB = mysql.memory_mb || 512;
  const poolMB = mysql.buffer_pool_mb || Math.floor(memMB / 2);
  const conns = mysql.max_connections || 100;
  const querySec = mysql.query_seconds ?? 60;
  const idleSec = mysql.idle_seconds || 300;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title={lang === 'bn' ? 'মারিয়াডিবি আর্কিটেকচার ও এন্টারপ্রাইজ টিউনিং গাইড' : 'MariaDB Architecture & Enterprise Tuning Guide'}
      description={
        lang === 'bn'
          ? 'NextDeploy-এর মাল্টি-টেন্যান্ট ডাটাবেজ সুরক্ষা, মেমোরি বাজেট ও কুয়েরি অপটিমাইজেশন'
          : 'NextDeploy multi-tenant database protection, memory budgeting, and query optimization'
      }
      icon={<MariaDbIcon className="h-5 w-5" />}
      maxWidth="2xl"
      footer={
        <div className="flex w-full items-center justify-between">
          <div className="flex items-center gap-1.5 text-[11px] text-zinc-400">
            <ShieldCheck className="h-4 w-4 text-emerald-400" />
            <span>{lang === 'bn' ? 'অটো-গার্ডরেল সক্রিয় · OOM ক্র্যাশ সুরক্ষিত' : 'Auto-guardrails active · OOM-Crash protected'}</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg bg-zinc-800 px-3.5 py-1.5 text-xs font-medium text-zinc-200 transition hover:bg-zinc-700 cursor-pointer"
          >
            {lang === 'bn' ? 'বুঝেছি / বন্ধ করুন' : 'Got it / Close'}
          </button>
        </div>
      }
    >
      <div className="space-y-4 text-zinc-300">
        {/* Language Pill Selector */}
        <div className="flex items-center justify-between rounded-xl border border-white/[0.07] bg-zinc-900/60 p-2 px-3">
          <div className="flex items-center gap-2 text-xs text-zinc-400">
            <BookOpen className="h-3.5 w-3.5 text-orange-400" />
            <span>{lang === 'bn' ? 'ভাষার বিকল্প নির্বাচন করুন:' : 'Documentation Language:'}</span>
          </div>
          <div className="flex items-center gap-1 rounded-lg bg-zinc-950 p-1 border border-white/[0.06]">
            <button
              type="button"
              onClick={() => setLang('bn')}
              className={`rounded px-2.5 py-1 text-xs font-medium transition cursor-pointer ${
                lang === 'bn' ? 'bg-orange-500 text-white shadow-sm' : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              বাংলা (BN)
            </button>
            <button
              type="button"
              onClick={() => setLang('en')}
              className={`rounded px-2.5 py-1 text-xs font-medium transition cursor-pointer ${
                lang === 'en' ? 'bg-orange-500 text-white shadow-sm' : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              English (EN)
            </button>
          </div>
        </div>

        {/* Live Hardware Telemetry Banner */}
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <div className="rounded-xl border border-white/[0.06] bg-zinc-900/40 p-2.5">
            <div className="flex items-center gap-1 text-[11px] text-zinc-400">
              <HardDrive className="h-3.5 w-3.5 text-blue-400" />
              <span>{lang === 'bn' ? 'সার্ভার মোট র‍্যাম' : 'Host Total RAM'}</span>
            </div>
            <div className="mt-1 font-mono text-sm font-semibold text-zinc-100">
              {hostMB ? `${hostMB} MB` : 'N/A'}
            </div>
            <div className="text-[10px] text-zinc-400">{hostMB ? `${(hostMB / 1024).toFixed(1)} GB` : ''}</div>
          </div>

          <div className="rounded-xl border border-white/[0.06] bg-zinc-900/40 p-2.5">
            <div className="flex items-center gap-1 text-[11px] text-zinc-400">
              <Database className="h-3.5 w-3.5 text-orange-400" />
              <span>{lang === 'bn' ? 'ডাটাবেজ বাজেট' : 'DB Memory Limit'}</span>
            </div>
            <div className="mt-1 font-mono text-sm font-semibold text-orange-400">
              {memMB} MB
            </div>
            <div className="text-[10px] text-zinc-400">{hostMB ? `${Math.round((memMB / hostMB) * 100)}% of Host` : 'Allocated'}</div>
          </div>

          <div className="rounded-xl border border-white/[0.06] bg-zinc-900/40 p-2.5">
            <div className="flex items-center gap-1 text-[11px] text-zinc-400">
              <Layers className="h-3.5 w-3.5 text-emerald-400" />
              <span>{lang === 'bn' ? 'বাফার পুল ক্যাশ' : 'InnoDB Buffer Pool'}</span>
            </div>
            <div className="mt-1 font-mono text-sm font-semibold text-emerald-400">
              {poolMB} MB
            </div>
            <div className="text-[10px] text-zinc-400">{lang === 'bn' ? 'বাজেটের ৫০%' : '50% of Container'}</div>
          </div>

          <div className="rounded-xl border border-white/[0.06] bg-zinc-900/40 p-2.5">
            <div className="flex items-center gap-1 text-[11px] text-zinc-400">
              <Zap className="h-3.5 w-3.5 text-amber-400" />
              <span>{lang === 'bn' ? 'কুয়েরি টাইমআউট' : 'Query Timeout'}</span>
            </div>
            <div className="mt-1 font-mono text-sm font-semibold text-amber-400">
              {querySec > 0 ? `${querySec}s` : (lang === 'bn' ? 'সীমাহীন' : 'Unlimited')}
            </div>
            <div className="text-[10px] text-zinc-400">{`Max ${conns} conns`}</div>
          </div>
        </div>

        {/* Documentation Sections */}
        {lang === 'bn' ? (
          <div className="space-y-3.5 text-xs leading-relaxed">
            {/* Section 1 */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">১</span>
                মেমোরি বাজেট ও ৫০% বাফার পুল নীতি (Memory & 50% Buffer Pool)
              </h3>
              <p className="mt-1.5 text-zinc-400">
                NextDeploy স্বয়ংক্রিয়ভাবে সার্ভারের মোট র‍্যামের এক-চতুর্থাংশ (২৫%) ডাটাবেজ কন্টেইনারকে বরাদ্দ করে (সর্বনিম্ন ৫১২ MB এবং সর্বোচ্চ ৪ GB)। আপনি চাইলে যেকোনো সময় এটি পরিবর্তন করতে পারবেন।
              </p>
              <div className="mt-2 rounded-lg bg-zinc-950/60 p-2.5 border border-white/[0.04]">
                <div className="font-medium text-zinc-300">কেন ১০০% বা পুরো মেমোরি বাফার পুলে দেওয়া হয় না?</div>
                <div className="mt-1 text-[11px] text-zinc-400">
                  InnoDB Buffer Pool-এ কন্টেইনার মেমোরির ঠিক ৫০% দেওয়া হয়, যাতে ঘন ঘন ব্যবহৃত টেবিল ও ইনডেক্স দ্রুত র‍্যামে ক্যাশ থাকে। বাকি ৫০% মেমোরি সুরক্ষিত রাখা হয় ক্লায়েন্ট কানেকশন, থ্রেড স্ট্যাক এবং সর্ট/জয়েন বাফারের জন্য। ফলে অতিরিক্ত ট্রাফিকেও লিনাক্স কার্নেলের OOM-Killer কখনো ডাটাবেজ ক্র্যাশ করতে পারে না।
                </div>
              </div>
            </div>

            {/* Section 2 */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">২</span>
                ভারী ও রানঅ্যাওয়ে কুয়েরি সুরক্ষা (Runaway Query Killer)
              </h3>
              <p className="mt-1.5 text-zinc-400">
                মাল্টি-টেন্যান্ট সার্ভারে কোনো একটি সাইটে আনইনডেক্সড সার্চ বা খারাপ কুয়েরি চললে তা CPU ১০০% করে পুরো সার্ভার ফ্রিজ করে দেয়।
              </p>
              <ul className="mt-2 space-y-1.5 text-[11px] text-zinc-400">
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">টেন্যান্ট কুয়েরি ৬০ সেকেন্ড লিমিট:</strong> প্রতিটি সাইটের ডাটাবেজ ইউজারের ওপর <code className="rounded bg-zinc-800 px-1 py-0.5 text-orange-300">MAX_STATEMENT_TIME 60</code> প্রযোজ্য। ৬০ সেকেন্ড পেরোলেই কুয়েরি বাতিল হবে।</span>
                </li>
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">phpMyAdmin ৩০০ সেকেন্ড:</strong> phpMyAdmin দিয়ে বড় টেবিল ইমপোর্ট করার জন্য ৫ গুণ বেশি সময় (৩০০ সেকেন্ড) বরাদ্দ থাকে।</span>
                </li>
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">রুট ও ব্যাকআপ সীমাহীন:</strong> প্যানেলের নিজস্ব ব্যাকআপ (<code className="rounded bg-zinc-800 px-1 py-0.5 text-orange-300">mariadb-dump</code>), মাইগ্রেশন বা রুট অপারেশনে কোনো সময়সীমা নেই—কাজ সম্পূর্ণ শেষ না হওয়া পর্যন্ত নির্বিঘ্নে চলবে।</span>
                </li>
              </ul>
            </div>

            {/* Section 3 */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">৩</span>
                কানেকশন স্কেলিং ও স্লিপিং কানেকশন নিষ্কাশন (Connection Pooling)
              </h3>
              <ul className="mt-2 space-y-1.5 text-[11px] text-zinc-400">
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">আইডল টাইমআউট ৩০০ সেকেন্ড (৫ মিনিট):</strong> PHP স্ক্রিপ্ট শেষ হওয়ার পর অবহেলায় পড়ে থাকা স্লিপিং কানেকশনগুলো ৫ মিনিটে মেরে মেমোরি খালি করা হয় (ডিফল্ট ৮ ঘণ্টার বদলে)।</span>
                </li>
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">ইউজার প্রতি ২৫ কানেকশন গার্ড:</strong> কোনো একটি হ্যাকড বা ক্র্যাশড সাইট ২৫টির বেশি কানেকশন নিয়ে অন্য সাইটের সংযোগ আটকাতে পারবে না (<code className="rounded bg-zinc-800 px-1 py-0.5 text-orange-300">MAX_USER_CONNECTIONS 25</code>)।</span>
                </li>
              </ul>
            </div>

            {/* Section 4 */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">৪</span>
                উন্নত প্রোডাকশন গার্ডরেল (Production Flags)
              </h3>
              <div className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-2 text-[11px]">
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">--skip-name-resolve</strong>
                  <div className="text-zinc-400 mt-0.5">রিভার্স DNS লুকআপ বন্ধ করে প্রতিটি ডাটাবেজ হ্যান্ডশেক লেটেন্সি একদম শূন্যে নামিয়ে আনে।</div>
                </div>
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">--max-allowed-packet=64M</strong>
                  <div className="text-zinc-400 mt-0.5">বড় ওয়ার্ডপ্রেস/লারাভেল ডাম্প এবং মিডিয়া ইমপোর্টে প্যাকেট সাইজ ফেইলিওর বন্ধ করে।</div>
                </div>
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">tmp_table_size (16–64M)</strong>
                  <div className="text-zinc-400 mt-0.5">ইন-মেমোরি অস্থায়ী টেবিল সাইজ বেঁধে দেয় যাতে বড় কার্টেসিয়ান জয়েনে মেমোরি শেষ না হয়।</div>
                </div>
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">utf8mb4_unicode_ci</strong>
                  <div className="text-zinc-400 mt-0.5">বাংলা, আন্তর্জাতিক অক্ষর ও আধুনিক ইমোজির পূর্ণ ৪-বাইট এনকোডিং সাপোর্ট করে।</div>
                </div>
              </div>
            </div>
          </div>
        ) : (
          <div className="space-y-3.5 text-xs leading-relaxed">
            {/* Section 1 EN */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">1</span>
                Memory Budget & 50% Buffer Pool Rule
              </h3>
              <p className="mt-1.5 text-zinc-400">
                NextDeploy automatically budgets 25% of total server RAM for the database container (minimum 512 MB, maximum 4 GB). You can manually customize this limit anytime.
              </p>
              <div className="mt-2 rounded-lg bg-zinc-950/60 p-2.5 border border-white/[0.04]">
                <div className="font-medium text-zinc-300">Why not allocate 100% of container memory to the Buffer Pool?</div>
                <div className="mt-1 text-[11px] text-zinc-400">
                  InnoDB Buffer Pool receives exactly 50% of the container memory to cache active tables and indexes in RAM. The remaining 50% is reserved as headroom for connection thread stacks, sort buffers, and join buffers. This prevents the Linux Kernel OOM (Out-of-Memory) Killer from abruptly crashing MariaDB under traffic spikes.
                </div>
              </div>
            </div>

            {/* Section 2 EN */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">2</span>
                Runaway & Heavy Query Protection
              </h3>
              <p className="mt-1.5 text-zinc-400">
                In multi-tenant environments, a single inefficient query (such as an unindexed wild-card search across millions of rows) can pin a CPU core at 100% and deadlock other sites.
              </p>
              <ul className="mt-2 space-y-1.5 text-[11px] text-zinc-400">
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">60-second Tenant Statement Timeout:</strong> Every tenant database user is granted <code className="rounded bg-zinc-800 px-1 py-0.5 text-orange-300">MAX_STATEMENT_TIME 60</code>. MariaDB automatically aborts any statement exceeding 60s.</span>
                </li>
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">phpMyAdmin 300s Window:</strong> phpMyAdmin sessions receive 5x allowance (300 seconds) for administrative schema migrations and imports.</span>
                </li>
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">Unlimited Root & Backups:</strong> System backups (<code className="rounded bg-zinc-800 px-1 py-0.5 text-orange-300">mariadb-dump</code>), panel migrations, and root maintenance operations are never throttled or interrupted.</span>
                </li>
              </ul>
            </div>

            {/* Section 3 EN */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">3</span>
                Connection Scaling & Idle Timeout Reclaiming
              </h3>
              <ul className="mt-2 space-y-1.5 text-[11px] text-zinc-400">
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">300s (5-Minute) Idle Reclaim:</strong> Abandoned or sleeping PHP connections are automatically closed after 5 minutes instead of the upstream 8-hour default, reclaiming connection slots.</span>
                </li>
                <li className="flex items-start gap-1.5">
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400 mt-0.5" />
                  <span><strong className="text-zinc-200">25 Connections per Tenant:</strong> Enforces <code className="rounded bg-zinc-800 px-1 py-0.5 text-orange-300">MAX_USER_CONNECTIONS 25</code> per website user to prevent connection starvation.</span>
                </li>
              </ul>
            </div>

            {/* Section 4 EN */}
            <div className="rounded-xl border border-white/[0.06] bg-zinc-900/30 p-3.5">
              <h3 className="flex items-center gap-2 font-semibold text-zinc-100">
                <span className="flex h-5 w-5 items-center justify-center rounded-md bg-orange-500/10 text-[11px] font-bold text-orange-400 border border-orange-500/20">4</span>
                Production Enterprise Guardrails
              </h3>
              <div className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-2 text-[11px]">
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">--skip-name-resolve</strong>
                  <div className="text-zinc-400 mt-0.5">Bypasses DNS reverse lookups on every client handshake, reducing connection latency to sub-millisecond.</div>
                </div>
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">--max-allowed-packet=64M</strong>
                  <div className="text-zinc-400 mt-0.5">Eliminates packet payload errors during large SQL restores and media blog posts.</div>
                </div>
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">tmp_table_size (16–64M)</strong>
                  <div className="text-zinc-400 mt-0.5">Restricts RAM exhaustion from unbounded Cartesian product joins or large GROUP BY operations.</div>
                </div>
                <div className="rounded-lg bg-zinc-950/40 p-2 border border-white/[0.04]">
                  <strong className="text-zinc-200 font-mono">utf8mb4_unicode_ci</strong>
                  <div className="text-zinc-400 mt-0.5">Full 4-byte UTF-8 encoding covering multilingual characters, Bengali typography, and emojis.</div>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </ShadcnDialog>
  );
};
