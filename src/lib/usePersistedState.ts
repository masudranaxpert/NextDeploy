import React, { useCallback, useState } from 'react';

export interface PersistedStateOptions<T> {
  /** Whitelist of accepted values; anything else in localStorage falls back to defaultValue. */
  allowed?: readonly T[];
  /** Runtime type guard for structured values (e.g. arrays/objects). */
  validate?: (value: unknown) => value is T;
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>;

function defaultStorage(): StorageLike | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function readStoredValue<T>(
  key: string,
  defaultValue: T,
  options?: PersistedStateOptions<T>,
  storage: StorageLike | null = defaultStorage(),
): T {
  if (!storage) return defaultValue;
  try {
    const raw = storage.getItem(key);
    if (raw === null) return defaultValue;

    let parsed: unknown;
    if (typeof defaultValue === 'boolean') {
      if (raw === 'true') parsed = true;
      else if (raw === 'false') parsed = false;
      else return defaultValue;
    } else if (typeof defaultValue === 'string') {
      parsed = raw;
    } else {
      parsed = JSON.parse(raw);
    }

    if (options?.allowed && !options.allowed.includes(parsed as T)) {
      return defaultValue;
    }
    if (options?.validate && !options.validate(parsed)) {
      return defaultValue;
    }
    return parsed as T;
  } catch {
    return defaultValue;
  }
}

export function writeStoredValue<T>(
  key: string,
  value: T,
  storage: StorageLike | null = defaultStorage(),
): void {
  if (!storage) return;
  try {
    const serialized =
      typeof value === 'string' || typeof value === 'boolean' || typeof value === 'number'
        ? String(value)
        : JSON.stringify(value);
    storage.setItem(key, serialized);
  } catch {
    // ignore storage errors (private browsing / quota exceeded)
  }
}

/** React state backed by localStorage with safe parsing and optional validation. */
export function usePersistedState<T>(
  key: string,
  defaultValue: T,
  options?: PersistedStateOptions<T>,
): [T, React.Dispatch<React.SetStateAction<T>>] {
  const [value, setValue] = useState<T>(() => readStoredValue(key, defaultValue, options));

  const setPersistedValue: React.Dispatch<React.SetStateAction<T>> = useCallback(
    (next) => {
      setValue((prev) => {
        const resolved = typeof next === 'function' ? (next as (p: T) => T)(prev) : next;
        writeStoredValue(key, resolved);
        return resolved;
      });
    },
    [key],
  );

  return [value, setPersistedValue];
}
