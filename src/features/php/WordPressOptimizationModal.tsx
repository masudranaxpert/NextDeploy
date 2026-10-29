import React, { useState } from 'react';
import { BookOpen, Check, FileCheck, Layers, ShieldCheck, Zap } from 'lucide-react';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { WordPressIcon } from '../../components/ui/BrandIcons';

interface WordPressOptimizationModalProps {
  open: boolean;
  onClose: () => void;
  siteName: string;
}

type TabKey = 'cache' | 'object-cache' | 'assets';

export const WordPressOptimizationModal: React.FC<WordPressOptimizationModalProps> = ({
  open,
  onClose,
  siteName,
}) => {
  const [activeTab, setActiveTab] = useState<TabKey>('cache');
  const [lang, setLang] = useState<'bn' | 'en'>('bn');

  const plugins = [
    {
      name: 'WP Fastest Cache',
      path: '/wp-content/cache/all/.../index.html',
      noteBn: 'সহজ সেটআপ, পেজ ক্যাশ ও বেসিক মিনিফিকেশন একসাথে কাজ করে।',
      noteEn: 'Simple setup, handles page caching and basic asset minification out of the box.',
    },
    {
      name: 'WP Super Cache',
      path: '/wp-content/cache/supercache/.../index-https.html',
      noteBn: 'অটোমেটিক স্ট্যাটিক ফাইল জেনারেটর, হাই-ট্রাফিকের জন্য নির্ভরযোগ্য।',
      noteEn: 'Official Automattic static file generator, battle-tested for high traffic.',
    },
    {
      name: 'Cache Enabler',
      path: '/wp-content/cache/cache-enabler/.../https-index.html',
      noteBn: 'লাইটওয়েট ডিস্ক ক্যাশ ইঞ্জিন, প্রিকমপ্রেসড Gzip সাপোর্ট রয়েছে।',
      noteEn: 'Lightweight disk cache engine with built-in precompressed Gzip support.',
    },
    {
      name: 'WP Rocket',
      path: '/wp-content/cache/wp-rocket/.../index-https.html',
      noteBn: 'প্রিমিয়াম অল-ইন-ওয়ান ক্যাশিং প্লাগইন।',
      noteEn: 'Comprehensive premium caching suite.',
    },
  ];

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title={lang === 'bn' ? 'ওয়ার্ডপ্রেস অপ্টিমাইজেশন ও পারফরম্যান্স' : 'WordPress Optimization & Performance'}
      description={
        lang === 'bn'
          ? `সাইট: ${siteName} · Caddy হাইপার-ক্যাশিং ও ট্রাফিক হ্যান্ডলিং গাইড`
          : `Site: ${siteName} · Caddy zero-overhead disk caching & traffic guide`
      }
      icon={<WordPressIcon className="h-5 w-5 text-[#3858E9]" />}
      maxWidth="2xl"
      footer={
        <div className="flex w-full items-center justify-between">
          <div className="flex items-center gap-1.5 text-[11px] text-zinc-400">
            <ShieldCheck className="h-4 w-4 text-emerald-400" />
            <span>
              {lang === 'bn'
                ? 'বাইপাস গার্ডরেল সক্রিয় · ডাইনামিক ডেটা নিরাপদ'
                : 'Bypass guardrails active · Dynamic data protected'}
            </span>
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
        {/* Language selector & tabs header */}
        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
          {/* Navigation Tabs */}
          <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-zinc-950 p-1">
            <button
              type="button"
              onClick={() => setActiveTab('cache')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition cursor-pointer ${
                activeTab === 'cache'
                  ? 'bg-zinc-800 text-white shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Zap className="h-3.5 w-3.5 text-amber-400" />
              <span>{lang === 'bn' ? 'স্ট্যাটিক ডিস্ক ক্যাশ' : 'Static Disk Cache'}</span>
            </button>
            <button
              type="button"
              onClick={() => setActiveTab('object-cache')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition cursor-pointer ${
                activeTab === 'object-cache'
                  ? 'bg-zinc-800 text-white shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Layers className="h-3.5 w-3.5 text-cyan-400" />
              <span>{lang === 'bn' ? 'অবজেক্ট ক্যাশ (Redis)' : 'Object Cache (Redis)'}</span>
              <span className="rounded bg-white/[0.06] px-1 py-0.2 font-mono text-[9px] text-zinc-400">Soon</span>
            </button>
            <button
              type="button"
              onClick={() => setActiveTab('assets')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition cursor-pointer ${
                activeTab === 'assets'
                  ? 'bg-zinc-800 text-white shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <FileCheck className="h-3.5 w-3.5 text-emerald-400" />
              <span>{lang === 'bn' ? 'অ্যাসেট মিনিফিকেশন' : 'Assets'}</span>
              <span className="rounded bg-white/[0.06] px-1 py-0.2 font-mono text-[9px] text-zinc-400">Soon</span>
            </button>
          </div>

          {/* Language Switch */}
          <div className="flex items-center gap-1 self-end sm:self-auto">
            <button
              type="button"
              onClick={() => setLang('bn')}
              className={`rounded px-2 py-1 text-[11px] font-medium transition cursor-pointer ${
                lang === 'bn' ? 'bg-orange-500/20 text-orange-400 font-semibold' : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              বাংলা
            </button>
            <span className="text-zinc-700">|</span>
            <button
              type="button"
              onClick={() => setLang('en')}
              className={`rounded px-2 py-1 text-[11px] font-medium transition cursor-pointer ${
                lang === 'en' ? 'bg-orange-500/20 text-orange-400 font-semibold' : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              English
            </button>
          </div>
        </div>

        {/* Tab 1: Static Disk Cache */}
        {activeTab === 'cache' && (
          <div className="space-y-4">
            {/* Overview Box */}
            <div className="rounded-xl border border-white/[0.08] bg-zinc-900/50 p-3.5">
              <div className="flex items-start gap-2.5">
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-amber-500/10 text-amber-400">
                  <Zap className="h-4 w-4" />
                </div>
                <div className="text-xs leading-relaxed">
                  <p className="font-medium text-zinc-100">
                    {lang === 'bn'
                      ? 'Caddy ডিরেক্ট ডিস্ক ক্যাশ কীভাবে কাজ করে?'
                      : 'How Caddy Zero-Overhead Disk Caching Works'}
                  </p>
                  <p className="mt-1 text-zinc-400">
                    {lang === 'bn'
                      ? 'আপনার ওয়ার্ডপ্রেস সাইটে নিচে উল্লেখিত ৪টি জনপ্রিয় ক্যাশিং প্লাগইনের যেকোনো একটি ইনস্টল করলেই হবে। প্লাগইন যখন পেজটির স্ট্যাটিক HTML ফাইল ডিস্কে সেভ করবে, Caddy স্বয়ংক্রিয়ভাবে সরাসরি তার ইন্টারনাল file_server দিয়ে ০.৫ মিলিসেকেন্ডে পেজটি ডেলিভার করবে। পিএইচপি (PHP-FPM) বা মারিয়াডিবি (MySQL) প্রসেস স্পর্শই হবে না।'
                      : 'Install any one of the four supported caching plugins below. Once the plugin writes static HTML files to disk, Caddy serves them directly via its built-in file_server in under 1ms, completely bypassing PHP-FPM and MariaDB.'}
                  </p>
                </div>
              </div>
            </div>

            {/* Supported Plugins Grid */}
            <div className="space-y-2">
              <div className="text-xs font-medium text-zinc-300">
                {lang === 'bn'
                  ? 'সমর্থিত প্লাগইনসমূহ (যেকোনো ১টি ব্যবহার করুন):'
                  : 'Supported Plugins (choose any one):'}
              </div>
              <div className="grid gap-2 sm:grid-cols-2">
                {plugins.map((p) => (
                  <div
                    key={p.name}
                    className="flex flex-col justify-between rounded-lg border border-white/[0.06] bg-zinc-950/70 p-3 transition hover:border-white/[0.12]"
                  >
                    <div>
                      <div className="flex items-center gap-1.5 font-medium text-zinc-100 text-xs">
                        <Check className="h-3.5 w-3.5 text-emerald-400" />
                        <span>{p.name}</span>
                      </div>
                      <p className="mt-1.5 text-[11px] leading-relaxed text-zinc-400">
                        {lang === 'bn' ? p.noteBn : p.noteEn}
                      </p>
                    </div>
                    <div className="mt-2.5 rounded bg-zinc-900/80 px-2 py-1 font-mono text-[10px] text-zinc-500 truncate">
                      {p.path}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Safety Guardrails */}
            <div className="rounded-xl border border-white/[0.08] bg-zinc-900/40 p-3.5">
              <div className="flex items-center gap-2 text-xs font-medium text-zinc-200">
                <ShieldCheck className="h-4 w-4 text-emerald-400" />
                <span>{lang === 'bn' ? 'স্বয়ংক্রিয় ক্যাশ বাইপাস রুলস' : 'Automatic Cache Bypass Guardrails'}</span>
              </div>
              <ul className="mt-2 space-y-1.5 text-[11.5px] text-zinc-400">
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    {lang === 'bn'
                      ? 'লগইন করা ইউজার বা অ্যাডমিন (wordpress_logged_in) আসলে ক্যাশ বাইপাস হয়ে লাইভ PHP-FPM এ যাবে।'
                      : 'Logged-in users or administrators (wordpress_logged_in) automatically bypass cache to PHP-FPM.'}
                  </span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    {lang === 'bn'
                      ? 'WooCommerce কার্টে পণ্য যোগ থাকলে (woocommerce_items_in_cart) রিয়েল-টাইম লাইভ সেশনে চলবে।'
                      : 'WooCommerce sessions with items in cart (woocommerce_items_in_cart) always run live.'}
                  </span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    {lang === 'bn'
                      ? 'POST রিকোয়েস্ট, সার্চ কুয়েরি (?s=...) বা অ্যাডমিন ডিরেক্টরি (/wp-admin/) ক্যাশ হবে না।'
                      : 'POST requests, search queries (?s=...), and /wp-admin/ paths are never cached.'}
                  </span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-400">✓</span>
                  <span>
                    {lang === 'bn'
                      ? 'কোনো প্লাগইন চালু না থাকলে বা ক্যাশ ফাইল না থাকলে সাধারণ PHP-FPM স্বাভাবিকভাবে চলবে।'
                      : 'If no cache plugin is configured, requests seamlessly route to standard PHP-FPM.'}
                  </span>
                </li>
              </ul>
            </div>
          </div>
        )}

        {/* Tab 2: Object Cache */}
        {activeTab === 'object-cache' && (
          <div className="rounded-xl border border-white/[0.08] bg-zinc-900/50 p-6 text-center">
            <Layers className="mx-auto h-8 w-8 text-cyan-400/80" />
            <h4 className="mt-3 text-sm font-medium text-zinc-200">
              {lang === 'bn' ? 'Redis অবজেক্ট ক্যাশ মডিউল' : 'Redis Object Cache Module'}
            </h4>
            <p className="mx-auto mt-1.5 max-w-md text-xs leading-relaxed text-zinc-400">
              {lang === 'bn'
                ? 'ওয়ার্ডপ্রেস ডাটাবেজ কুয়েরি রিডিউস করতে Redis ব্যাকএন্ড কন্টেইনার এবং মেমোরি ক্যাশিং খুব শীঘ্রই আসছে।'
                : 'Shared Redis container and persistent in-memory object cache wiring for database queries will be available in the upcoming release.'}
            </p>
          </div>
        )}

        {/* Tab 3: Assets */}
        {activeTab === 'assets' && (
          <div className="rounded-xl border border-white/[0.08] bg-zinc-900/50 p-6 text-center">
            <FileCheck className="mx-auto h-8 w-8 text-emerald-400/80" />
            <h4 className="mt-3 text-sm font-medium text-zinc-200">
              {lang === 'bn' ? 'স্ট্যাটিক অ্যাসেট মিনিফিকেশন ও কমপ্রেশন' : 'Static Asset Minification & Precompression'}
            </h4>
            <p className="mx-auto mt-1.5 max-w-md text-xs leading-relaxed text-zinc-400">
              {lang === 'bn'
                ? 'CSS, JS ও ইমেজ অ্যাসেট অটো-মিনিফিকেশন এবং Gzip / Zstandard কমপ্রেশন গাইডলাইন পরবর্তী আপডেটে সংযুক্ত করা হবে।'
                : 'Asset pipeline rules and Gzip / Zstandard precompression directives will be documented here in the next update.'}
            </p>
          </div>
        )}
      </div>
    </ShadcnDialog>
  );
};
