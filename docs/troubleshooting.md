# Troubleshooting & known issues

This document collects operational quirks and workarounds. Add new entries here as we discover them.

---

## Volume restore: large backup uploads behind Cloudflare

**Symptom:** Restoring a Docker volume from a large `.tar.gz` or `.zip` (e.g. 250 MB) appears stuck at a low upload percentage (e.g. 1%) or fails before extraction starts.

**Cause:** The NextDeploy panel accepts large uploads (see `BodyLimit` in the app; up to **2 GiB**). If your panel hostname is **proxied through Cloudflare** (orange cloud), Cloudflare enforces a **maximum request body size** on the edge **before** traffic reaches your server. Typical limits:

| Cloudflare plan | Approx. max upload body |
|-----------------|-------------------------|
| Free / Pro      | **100 MB**              |
| Business        | **200 MB**              |
| Enterprise      | **500 MB** (default; can be raised) |

**Workaround:** Bypass Cloudflare for the upload so the request hits your origin directly:

1. Open the panel using the server **IP and published port** (default panel port is **8080** unless you changed it), for example:
   - `http://YOUR_SERVER_IP:8080`
2. Log in and run **Restore from backup** on **Volumes → Browse** as usual.
3. Alternatively, create a **DNS-only** (grey cloud) hostname that points straight to your server and use that URL only for large uploads.

**Note:** Direct IP access may use HTTP unless you terminate TLS elsewhere. Use this path only where you trust the network (or VPN / SSH tunnel).

---

## Volume restore: safe order for an app (containers → restore → deploy)

**When to follow this:** You are restoring a Docker volume that an app’s stack still uses (for example Postgres/MySQL data, uploads, or any bind-mounted named volume). Running containers can keep files open or rewrite the volume while restore runs, which leads to corruption, permission errors, or confusing failures after restore.

**Recommended workflow:**

1. Open the **app** in the panel and go to the **Containers** tab.
2. **Remove** every container that belongs to that app (use the container remove actions there so nothing is still running against the volume).
3. Go to **Volumes → Browse**, select the correct volume, and run **Restore from backup** (prefer the `.tar.gz` from **Download full backup** for databases).
4. Return to the app and **Deploy** again so Compose recreates containers against the restored data.

Skipping step 2 often causes issues with database volumes (e.g. Postgres `pg_logical` permission or checkpoint errors) because the old process tree still expects the old on-disk state.

---


