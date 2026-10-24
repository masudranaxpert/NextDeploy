# NextDeploy PHP Hosting Module (CloudPanel-এর মতো) — আর্কিটেকচার প্রস্তাব

> স্ট্যাটাস: প্রস্তাব · তারিখ: 2026-10-24 · আগের সাধারণ plugin ডিজাইন: [plugin-system-design.md](plugin-system-design.md)

## 0. লক্ষ্য

NextDeploy-তে একটা "PHP Sites" অংশ, যেটা CloudPanel-এর মতো কাজ করবে:

- User **NextDeploy-এর লগইন দিয়েই** ঢুকবে। আলাদা কোনো প্যানেল বা লগইন থাকবে না।
- Site তৈরি (domain + PHP version), Database (MySQL/MariaDB), File Manager, Cron Jobs, SFTP, SSL, Logs, Backups, এক ক্লিকে WordPress।
- Admin যে limit দিয়েছে (site সংখ্যা, RAM, CPU, storage, database সংখ্যা) তার বাইরে যাওয়া যাবে না।
- Nginx না, বিদ্যমান **Caddy** ব্যবহার হবে।

---

## 1. মূল প্রশ্ন: host-এ native install, নাকি Docker?

### CloudPanel কিভাবে কাজ করে (native মডেল)
- প্রতি site-এর জন্য একটা Linux user (যেমন `emwbd`), home `/home/emwbd/htdocs/<domain>`।
- প্রতি site-এর জন্য আলাদা PHP-FPM pool, যেটা ওই Linux user হিসেবে চলে। Nginx FastCGI দিয়ে PHP request পাঠায়।
- পুরো সার্ভারে একটাই MySQL। প্রতি site-এর আলাদা DB ও DB user।
- FTP/SSH: ওই Linux user দিয়ে। Cron: ওই user-এর crontab।
- এজন্যই CloudPanel একটা **খালি, শুধু তার জন্য রাখা** সার্ভার চায়। পুরো OS (nginx, php, mysql, varnish) সে নিজে নিয়ন্ত্রণ করে।

### NextDeploy-তে native মডেল কেন খাটবে না
| সমস্যা | কারণ |
|---|---|
| Port 80/443 দখল | এখন Caddy container 80/443 ধরে আছে। Host-এ nginx বসালে সংঘর্ষ হবে। |
| Panel নিজেই container-এ | Host-এ package install, Linux user বানানো, crontab লেখার জন্য root-privileged host agent লাগবে। এটা বড় নিরাপত্তা ঝুঁকি। |
| OS-নির্ভরতা | Ubuntu 22/24, Debian, প্রতি PHP version-এর repo আলাদাভাবে সামলাতে হবে। |
| বিদ্যমান ফিচার ভাঙবে | Migration, backup, rollback, cgroup limit সবই Docker-ভিত্তিক। |
| দুইটা ম্যানেজমেন্ট সিস্টেম | একই সার্ভারে Docker আর native stack একসাথে চালানো hosting industry-তে খারাপ অভ্যাস বলে পরিচিত। |

### Docker-এ কি PHP hosting সম্ভব? হ্যাঁ, এবং এটাই এখন ট্রেন্ড
- **Enhance** (আধুনিক cPanel বিকল্প): প্রতি site-এর PHP, SSH, cron আলাদা container-এ চালায়।
- **OpenPanel**: প্রতিটা service আলাদা container-এ, CPU/RAM limit সহ।
- **EasyPanel** ও **wpcontroller**: site-প্রতি PHP container।
- Container কোনো VM না। একই kernel ব্যবহার করে, তাই PHP-FPM-এর গতি native-এর প্রায় সমান। Site-প্রতি অতিরিক্ত খরচ শুধু idle RAM, `pm=ondemand` দিলে মোটামুটি ১০-২০ MB।
- বরং isolation **CloudPanel-এর চেয়ে ভালো**: প্রতিটা site শুধু নিজের ফোল্ডার দেখতে পায়, নিজের cgroup-এ চলে।

**সিদ্ধান্ত: CloudPanel-এর মডেলটাই নেবো, শুধু প্রতিটা অংশ container-এ চলবে।**

| CloudPanel (native) | NextDeploy (container) |
|---|---|
| Linux user প্রতি site | প্রতি site আলাদা UID (`10000 + site_no`), container সেই UID-তে চলে |
| PHP-FPM pool প্রতি site | **PHP-FPM pool প্রতি site**, user×version container-এর ভেতরে (§2) |
| Nginx vhost | **Caddy label** (`root` + `php_fastcgi` + `file_server`) |
| একটা MySQL | **একটা `nd-mysql` container** (MariaDB 11 / MySQL 8), প্রতি site-এর আলাদা DB ও user |
| ProFTPD / SSH user | **SFTPGo container**, প্রতি site-এর আলাদা SFTP user (chroot = site folder) |
| User crontab | **Panel-এর scheduler** (`robfig/cron` আগে থেকেই আছে) → `docker exec` site UID হিসেবে |
| Varnish | দরকার নেই প্রথমে। পরে Caddy cache-handler বা Redis object cache। |
| Let's Encrypt | Caddy নিজে থেকেই করে |

---

## 2. Runtime মডেল (চূড়ান্ত): প্রতি site-এ container না, প্রতি version-এ image

> আগের খসড়ায় ছিল "প্রতি site একটা PHP container + blue/green slot"। সেটা **বাতিল**। PHP site-এ slot থাকবে না।

### 2.1 Admin: plugin চালু → Instance পেজ
Admin "PHP Hosting" module চালু করলে "PHP Panel → Instance" পেজ আসবে:
- **MySQL:** চালু/বন্ধ, version (MariaDB 11.4 / MySQL 8.4)। চালু করলে root stack-এ `nd-mysql` container যোগ হবে (যেমন এখন `caddy` আছে)।
- **PHP versions:** checkbox: 7.4 · 8.1 · 8.2 · 8.3 · 8.4 · 8.5। যেগুলো টিক দেওয়া হবে, শুধু সেগুলোর image pull হবে (`nextdeploy/php:8.3` ইত্যাদি)। কোনো version-এ site চললে সেটা বন্ধ করা যাবে না।
- **Services তালিকা** (CloudPanel-এর মতো): MySQL, PHP-FPM 8.3, 8.4 … status + Restart।

### 2.2 Image বনাম container: সাইজের প্রশ্ন
- **Image সার্ভারে একবারই ডাউনলোড হয়।** একই image থেকে ১০০টা container চালালেও disk-এ image একটাই থাকে (Docker layer share করে)। তাই খরচটা "প্রতি version-এ একটা image", site-এর সংখ্যার উপর নির্ভর করে না।
- Image ছোট রাখার উপায়: `php:8.x-fpm-alpine` base + `install-php-extensions` দিয়ে দরকারি extension (mysqli, pdo_mysql, opcache, gd, intl, zip, bcmath, exif, redis, imagick), build dependency মুছে ফেলা। প্রতিটা ≈ ৬০–৯০MB compressed, ≈ ২০০MB disk। ৪টা version ≈ ০.৮GB। CloudPanel native-এ ১০টা PHP version install করলেও প্রায় এতটাই জায়গা লাগে।
- **একটা container-এ ৪টা FPM?** করা যায় (Ubuntu + ondrej PPA + supervisord), কিন্তু **না করাই ভালো**: image ৬০০MB+, একটা version আপডেট করতে সবগুলো restart করতে হয়, একটা crash করলে সবগুলো পড়ে যায়, আর disk-ও বাঁচে না (প্রতিটা version-এর binary আলাদাই)। → **প্রতি version-এর জন্য আলাদা image।**
- একই version কয়টা চালানো যাবে? যতগুলো দরকার। একই image থেকে যতগুলো খুশি container চালানো যায়।

### 2.3 Container: প্রতি user × প্রতি version (CloudPanel-এর "pool per site"-এর মতো)
```
nd-php83-u7   (user 7-এর সব 8.3 site)  ← user 7-এর cgroup slice-এ (RAM/CPU quota)
   ├─ pool site-12  (UID 20012, open_basedir = site-12 ফোল্ডার)
   ├─ pool site-15  (UID 20015)
nd-php84-u7   (user 7-এর 8.4 site)  ← শুধু তখনই চলবে যখন user-এর অন্তত একটা 8.4 site থাকবে
nd-php83-u9   (user 9)
nd-mysql      (সবার জন্য একটা)
```
- কেন প্রতি user-এর জন্য আলাদা: তাহলে user-এর RAM/CPU limit kernel নিজেই enforce করে (বিদ্যমান cgroup slice)। সব user-এর জন্য একটা shared 8.3 container থাকলে এটা সম্ভব হতো না।
- Container-এর সংখ্যা = user × তার ব্যবহৃত version (সাধারণত user-প্রতি ১টা), site-এর সংখ্যা না। Idle অবস্থায় (`pm=ondemand`) প্রায় ১০MB।
- Container-এ mount: `data/workspaces/php/u7/` (user-এর সব site একসাথে, পুরো ফোল্ডার)। ফলে নতুন site যোগ করলে container আবার তৈরি করতে হয় না।
- প্রতিটা site আলাদা FPM pool, আলাদা UID-তে চলে, `open_basedir` দিয়ে শুধু নিজের ফোল্ডার দেখতে পায়। Container-এ `cap_drop: ALL` + শুধু `SETUID/SETGID`।
- বিকল্প (সহজ, কিন্তু quota দুর্বল): প্রতি version-এর জন্য সবার একটা shared container। RAM-এর limit তখন শুধু `pm.max_children × memory_limit` দিয়ে আন্দাজে ঠিক করা যায়, আর CPU-র কোনো limit থাকে না।

### 2.4 User site খুললে কী হয় (কয়েক সেকেন্ড, কোনো image pull নেই)
1. Quota চেক (`max_apps`) → app row (`kind=php`) → ফোল্ডার `data/workspaces/php/u7/site-42/htdocs`।
2. `nd-php83-u7` না থাকলে তৈরি হবে (image আগে থেকেই আছে)।
3. Pool file লেখা `…/pools/site-42.conf` → FPM-কে `USR2` signal (graceful reload, চলমান request ভাঙে না)।
4. Caddy route যোগ → preview domain দিয়ে সাথে সাথে খোলা যাবে। Domain পরে যোগ হবে।

### 2.5 PHP version বদলানো (8.3 → 8.4), slot ছাড়াই, downtime ছাড়াই
1. `nd-php84-u7` নিশ্চিত করা → সেখানে site-42-এর pool লেখা → reload।
2. Caddy route-এর socket বদলানো (`…/84/site-42.sock`)।
3. 8.3 container থেকে pool মুছে reload। সেখানে আর কোনো pool না থাকলে container বন্ধ।
ফাইল আর DB কিছুই নড়ে না। CloudPanel-ও ঠিক এভাবে pool এক FPM থেকে আরেকটায় সরায়।

### 2.6 Request কিভাবে যাবে
```
Browser → Caddy ─ static (css/js/ছবি) → data/workspaces/php/u7/site-42/htdocs (Caddy-তে :ro, আগে থেকেই mount করা)
                └ *.php → php_fastcgi unix//run/nd-php/u7/83/site-42.sock
                               (shared volume `nd-php-sock`, Caddy আর PHP container দুটোতেই mount)
                                        └→ nd-mysql:3306 (network nd-db)
```
- FastCGI কোনো TCP port-এ খোলা থাকবে না, শুধু unix socket।
- **Caddy route কোথায় থাকবে:** site-এর নিজের কোনো container নেই, তাই label বসানোর জায়গা নেই। সমাধান হলো একটা ছোট "route holder" container `nd-php-routes` (pause image, ≈১MB, কিছুই চালায় না), যার label-এ সব PHP site-এর route থাকবে (`caddy_0`, `caddy_1`, …)। Domain যোগ বা বদল হলে শুধু এই container আবার তৈরি হবে, আর caddy-docker-proxy gracefully reload করবে। বিকল্প হলো `CADDY_DOCKER_CADDYFILE_PATH` base Caddyfile-এ `import` করা, কিন্তু প্রতিবার regenerate-এর সময় ফাইলটা আবার পড়া হয় কিনা নিশ্চিত না। Phase 0-তে পরীক্ষা করে দেখতে হবে।
- Path: Caddy আর PHP container-এ একই absolute path থাকতে হবে (`SCRIPT_FILENAME` মেলানোর জন্য)।

## 3. Authentication — NextDeploy-এর লগইনই একমাত্র লগইন

- **Web UI:** "PHP Sites" NextDeploy-এর ভেতরের একটা পেজ (sidebar-এ নতুন মেনু)। সব API `/api/php/*` বা `/sites/*`-এ থাকবে, বিদ্যমান `AuthMiddleware` + `RequireAppAccess`-এর পেছনে। আলাদা কোনো লগইন নেই, আলাদা কোনো সার্ভারও নেই।
- **Site মালিকানা:** একটা PHP site আসলে NextDeploy-এর একটা `app` (`kind = 'php'`)। তাই বিদ্যমান owner/collaborator RBAC (viewer/developer) সরাসরি কাজ করবে।
- **SFTP:** FileZilla-র মতো ক্লায়েন্ট web session ব্যবহার করতে পারে না, তাই CloudPanel-এর মতো **site-প্রতি SFTP user + password/SSH key** থাকবে (screenshot-এর "Site User Settings")। Password NextDeploy DB-তে (bcrypt) রাখা হবে। SFTPGo-র **external auth hook** NextDeploy-কে জিজ্ঞেস করবে (`POST /internal/sftp/auth`, শুধু internal network থেকে)। ফলে user-এর তথ্য একটাই জায়গায় থাকবে, NextDeploy-এ।
- **phpMyAdmin/Adminer:** প্রতি site-এর DB-র জন্য "Open in Adminer" বাটন। NextDeploy একটা এককালীন সাইন-অন টোকেন দেবে, Adminer আলাদা কোনো password চাইবে না।
- **WordPress admin:** WordPress-এর নিজের লগইন থাকবে (এটা site-এর end-user-দের জন্য, প্যানেলের অংশ না)।

---

## 4. Limit / Quota

`users` টেবিলে নতুন কলাম (admin "Team & Quotas" পেজ থেকে সেট করবে):

| Limit | কোথায় enforce হবে |
|---|---|
| `max_sites` (অথবা বিদ্যমান `max_apps`-এর ভেতরেই গোনা) | site তৈরির সময়, atomic transaction-এ (`BEGIN IMMEDIATE`) |
| `max_databases` | DB তৈরির সময় |
| `max_cron_jobs`, সর্বনিম্ন interval (যেমন ৫ মিনিট) | cron সেভ করার সময় |
| `max_memory_mb`, `max_cpus` (বিদ্যমান) | PHP container user-এর cgroup slice-এ চলে, তাই সব site মিলিয়ে এর বেশি নিতে পারবে না |
| `max_storage_mb` (বিদ্যমান) | site ফাইল + **DB size** (`information_schema` থেকে) মিলিয়ে। Upload/File Manager-এ বিদ্যমান `CheckStorageQuota` |
| DB size পার হলে | MySQL-এ DB-প্রতি size limit নেই। তাই প্রতি ১০ মিনিটে চেক করবে, limit পার হলে DB user-এর `INSERT/UPDATE` grant সরিয়ে নেবে (cPanel-ও এভাবে করে), user-কে নোটিফিকেশন দেবে |
| `MAX_USER_CONNECTIONS` | প্রতিটা DB user-এর জন্য (যেমন ২০), যাতে এক site পুরো MySQL আটকে দিতে না পারে |

> আগের রিসার্চে পাওয়া বাগ: `max_apps` চেক এখন `apps.go` আর `mcp/handler.go`-তে কপি করা, আর transaction ছাড়া। PHP site-ও যেহেতু app, তাই প্রথমে এটাকে একটা `internal/quota` service-এ আনতে হবে।

---

## 5. CloudPanel-এর প্রতিটা ট্যাব NextDeploy-তে কিভাবে হবে

| CloudPanel ট্যাব | NextDeploy-তে বাস্তবায়ন | বিদ্যমান কোড reuse |
|---|---|---|
| **Settings** (domain, root dir) | site meta + Caddy label regenerate | `internal/caddy`, domain handlers |
| **Vhost** | Caddy label editor (redirect, headers, custom rules), "advanced" মোডে কাঁচা Caddyfile snippet (validate করে) | `GenerateServiceLabels` |
| **Databases** | `nd-mysql`-এ `CREATE DATABASE` / `CREATE USER ... MAX_USER_CONNECTIONS`, Adminer SSO, import/export (.sql.gz) | — (নতুন `internal/phphosting/mysql.go`) |
| **Varnish Cache** | Phase 3: Caddy cache বা Redis object cache toggle | — |
| **SSL/TLS** | Caddy auto (Let's Encrypt), custom cert upload | বিদ্যমান |
| **Security** | IP allow/deny, Basic Auth (Caddy `basic_auth`), bot block | Caddy labels |
| **SSH/FTP** | SFTPGo user (password + SSH key), web terminal (site container-এ exec, site UID হিসেবে) | `ws_terminal.go` |
| **File Manager** | বিদ্যমান workspace file manager, শুধু `htdocs`-এ সীমাবদ্ধ, owner UID ঠিক রাখবে | `workspace_files.go`, `api_files.go` |
| **Cron Jobs** | Panel scheduler → `docker exec -u <uid> site-<id>-php <cmd>`, timeout ও log সহ | `robfig/cron` (backup_worker) |
| **Logs** | Caddy access log (site-প্রতি ফিল্টার) + PHP error log | `ws_logs.go`, `internal/logview` |
| **Backups** | site ফাইল + `mysqldump` → বিদ্যমান backup destinations (rclone) | `internal/backup`, `internal/rclone` |
| **Admin → Instance → Services** | `nd-mysql`, `sftpgo`, `caddy`, প্রতিটা PHP version-এর container-এর status/restart | Containers view |

### এক ক্লিকে WordPress
1. Site তৈরি (PHP 8.3) → 2. DB + DB user তৈরি (random password) → 3. এককালীন `wordpress:cli` container চালানো (site UID, একই mount, `nd-db` network):
   `wp core download` → `wp config create` → `wp core install --url --title --admin_user --admin_email` →
4. Admin password একবার দেখানো হবে → 5. WordPress-এর জন্য তৈরি Caddy rules (`/wp-content/uploads/*.php` ব্লক, `xmlrpc.php` ঐচ্ছিক ব্লক)।

পরে একই প্যাটার্নে Laravel, Joomla, Drupal, PrestaShop আনা যাবে। প্রতিটা হবে একটা YAML "recipe"।

---

## 6. কোড কোথায় থাকবে: plugin, নাকি core?

এই PHP hosting অংশটা Caddy label, quota, cgroup, backup আর file manager-এর সাথে গভীরভাবে জড়িত। তাই এটা আলাদা রিপোর external plugin হিসেবে না বানিয়ে **NextDeploy রিপোর ভেতরে একটা built-in module** হিসেবে বানানো ভালো, যেটা Settings থেকে চালু/বন্ধ করা যাবে:

```
internal/phphosting/      sites.go  mysql.go  sftp.go  cron.go  wordpress.go  compose.go
internal/quota/           একক limit service
src/features/php-sites/   SitesList, SiteSettings, Databases, CronJobs, SftpUsers ...
images/php/               nextdeploy/php:7.4..8.4-fpm Dockerfile (GitHub Actions দিয়ে build)
```

- এটাকে চালু করলে প্রথমবার `nd-mysql` আর `sftpgo` container root stack-এ যোগ হবে (যেমন এখন Caddy হয়)। বন্ধ করলে UI লুকিয়ে যাবে, বিদ্যমান site চলতেই থাকবে।
- আগের ডকুমেন্টের container-plugin সিস্টেম পরে third-party extension-এর জন্য থাকবে। PHP hosting-এর জন্য এটা দরকার নেই, তাই সেটা অপেক্ষা করতে পারে।

---

## 7. ফেজ প্ল্যান

**Phase 0:** `internal/quota` (atomic `max_apps`), Caddy version pin, `nextdeploy/php` image (৮.১–৮.৪)।

**Phase 1 — MVP:** site তৈরি/মুছা, PHP version বদলানো, domain + SSL, File Manager (htdocs), `nd-mysql` + DB তৈরি, quota (`max_sites`, `max_databases`, RAM/CPU/storage)।

**Phase 2:** Cron Jobs, SFTP (SFTPGo + auth hook), এক ক্লিকে WordPress, Adminer SSO, Logs, Backup (ফাইল + DB)।

**Phase 3:** Vhost/Security editor, cache, আরও recipe (Laravel ইত্যাদি), DB size enforcement, Admin "Services" পেজ, CloudPanel থেকে import।

---

## 8. ঝুঁকি

| ঝুঁকি | প্রতিরোধ |
|---|---|
| এক site আরেক site-এর ফাইল পড়া | প্রতি container-এ শুধু নিজের folder mount, আলাদা UID, `read_only` root FS |
| PHP থেকে Docker/host দখল | docker.sock নেই, `cap_drop: ALL`, `no-new-privileges`, `disable_functions` (exec, shell_exec ইত্যাদি, ঐচ্ছিক) |
| এক site পুরো সার্ভার খেয়ে ফেলা | container limit + user cgroup slice + `MAX_USER_CONNECTIONS` |
| MySQL একটা single point of failure | healthcheck, auto-restart, প্রতিদিন dump backup |
| অনেক site মানে অনেক container | `pm=ondemand`, idle-এ ~১০-২০MB। ২০GB RAM-এর সার্ভারে কয়েকশো site সম্ভব |
| SFTP auth hook spoof | hook endpoint শুধু internal network-এ, shared secret সহ |

## 9. UI স্পেসিফিকেশন: লাইভ CloudPanel v2.5.4 দেখে বানানো

২০২৬-১০-২৪ তারিখে একটা লাইভ CloudPanel instance শুধু পড়ার জন্য ঘুরে দেখা হয়েছে। কোনো কিছু save, delete বা restart করা হয়নি। প্রতিটা স্ক্রিনের আসল ফিল্ড নিচে দেওয়া আছে, পাশে NextDeploy-তে সেটা কিভাবে হবে।

### 9.1 গুরুত্বপূর্ণ পর্যবেক্ষণ
- **CloudPanel-এ কোনো quota বা limit নেই।** User-এর জন্য শুধু role (Admin / Site Manager / User) আর কোন কোন site সে দেখতে পাবে, সেটা ঠিক করা যায়। Site সংখ্যা, RAM, disk কিছুই সীমিত করা যায় না। **এখানে NextDeploy-এর বিদ্যমান quota সিস্টেম একটা বড় সুবিধা।**
- Site-এর ধরন ৬টা: WordPress, PHP, Node.js, Static HTML, Python, Reverse Proxy। NextDeploy-তে Node.js, Python আর Reverse Proxy আগে থেকেই Docker app হিসেবে আছে। তাই PHP module-এ শুধু **WordPress, PHP, Static** লাগবে। "Add Site" পেজে বাকি তিনটা বিদ্যমান app তৈরির পেজে নিয়ে যাবে।
- PHP site-এর "Application" ড্রপডাউনে ৩০টা preset আছে (Laravel, Symfony, Magento, Drupal, WooCommerce, WHMCS…)। এগুলো আসলে শুধু **vhost template** (document root, rewrite rules)। আমাদের ক্ষেত্রে এগুলো হবে Caddy recipe (YAML)।
- CloudPanel restart আর delete করে **GET link + token** দিয়ে। আমরা POST + CSRF ব্যবহার করবো, যেমন এখন করি।
- Admin অংশের বেশিরভাগ (Dashboard, Events, Backups, Users, Settings) NextDeploy-তে **আগে থেকেই আছে**: Monitoring, Audit Trail, Backup (rclone), Team & Quotas, Settings।

### 9.2 Route আর নেভিগেশন
```
Sidebar → "PHP Sites" (নতুন, Projects-এর পাশে)
/sites                         সব site-এর তালিকা
/sites/new                     ধরন বাছাই (WordPress | PHP | Static | → Node/Python/Proxy বিদ্যমান app ফর্মে যাবে)
/sites/new/wordpress|php|static
/sites/:siteId/:tab            tab = settings | caddy | databases | ssl | security | access | files | cron | logs | backups
```
Site পেজের হেডার (CloudPanel-এর মতো): **Domain** (বাইরের লিংক সহ) · **Site User** · **IP Address** · নতুন: **PHP version badge** · **Quota ব্যবহার** (RAM/Disk-এর ছোট বার)।

### 9.3 Sites তালিকা
কলাম: Domain · Site User · ধরন (PHP/WordPress/Static) · PHP version · Status (Running/Stopped) · Disk · Action (Manage)। উপরে **"+ Add Site"** বাটন। User-এর limit শেষ হয়ে গেলে বাটন বন্ধ থাকবে, সাথে লেখা থাকবে "৫টার মধ্যে ৫টা site ব্যবহার হয়েছে"।

### 9.4 নতুন site তৈরি: শুধু নাম দিয়ে (আপডেট, ইউজারের সিদ্ধান্ত)
- ফর্মে থাকবে **শুধু Site Name** (+ ধরন: PHP / WordPress / Static, + PHP version ডিফল্ট 8.3)।
- **Site User / Password / RAM / CPU ফিল্ড থাকবে না।** User আগে থেকেই NextDeploy-তে আছে, তার quota admin ঠিক করে দিয়েছে। Site container নিজে থেকেই ওই user-এর cgroup slice-এ চলবে।
- **Domain পরে যোগ হবে**, একাধিক domain যোগ করা যাবে। এর জন্য বিদ্যমান domain সিস্টেম ব্যবহার হবে। তৈরির সাথে সাথে একটা অস্থায়ী preview domain দেওয়া হবে (বিদ্যমান wildcard/preview domain)।
- WordPress হলে admin user/email/password তৈরির পরে "Install WordPress" ধাপে চাওয়া হবে, অথবা নিজে থেকে তৈরি করে একবার দেখানো হবে।
- SFTP দরকার হলে Access ট্যাব থেকে আলাদাভাবে চালু করতে হবে (ডিফল্ট বন্ধ)।

নিচের CloudPanel-এর ফর্মগুলো শুধু তুলনার জন্য রাখা হলো:

| ফর্ম | ফিল্ড (CloudPanel) | NextDeploy-তে পরিবর্তন |
| ফর্ম | ফিল্ড (CloudPanel) | NextDeploy-তে পরিবর্তন |
|---|---|---|
| WordPress | Domain Name, Site Title, Site User, Site User Password (generate), Admin User Name, Admin Password (generate), Admin E-Mail, Multisite (No/Yes) | একই। সাথে PHP version (ডিফল্ট 8.3)। DB নিজে থেকে তৈরি হবে। Multisite পরে আসবে। |
| PHP | Application (৩০টা preset), Domain Name, PHP Version (8.5…7.1), Site User, Site User Password | প্রথমে preset শুধু Generic, WordPress, Laravel, Symfony, Drupal, Joomla, WooCommerce। PHP version ৭.৪–৮.৫। |
| Static | Domain, Site User, Password | একই। PHP container লাগবে না, শুধু Caddy `file_server`। |

ফর্মের নিচে quota-র সারাংশ দেখাবে, যেমন: "এই site-এ ডিফল্ট 256MB RAM, আপনার বাকি আছে 768MB"।

### 9.5 Site ট্যাবগুলো
**Settings**
- Domain Settings: Domain Name (শুধু পড়ার জন্য), Root Directory (htdocs-এর সাপেক্ষে, যেমন `public`)। নিচে পুরো path দেখাবে।
- Site User Settings: Site User (শুধু পড়ার জন্য), Password (generate), SSH Keys (textarea)।
- PHP Settings: PHP Version, `memory_limit` (64MB–5GB), `max_execution_time` (15s–1h), `max_input_time`, `max_input_vars` (1000–100000), `post_max_size` আর `upload_max_filesize` (2MB–5GB), Additional php.ini directives (textarea)।
  - আমাদের ক্ষেত্রে এগুলো একটা `zz-site.ini` ফাইলে লেখা হবে, তারপর container reload হবে।
  - **`memory_limit` কখনো site container-এর memory limit বা user quota-র চেয়ে বেশি দেওয়া যাবে না।** ড্রপডাউনে শুধু অনুমোদিত মানগুলো থাকবে।
- PageSpeed: বাদ (Caddy-তে এটা নেই)। এর জায়গায় "Compression (gzip/zstd)" টগল।
- Danger zone: **Delete Site**। নিশ্চিত করতে domain টাইপ করতে হবে (CloudPanel-এর মতো)।

**Vhost → "Caddy Config"**
- CloudPanel-এ এখানে পুরো nginx vhost textarea। আমাদের এখানে দুই ভাগ থাকবে: (ক) ফর্ম: redirect www↔non-www, force HTTPS, custom headers, rewrite rules; (খ) "Advanced": Caddy snippet textarea। Save-এর আগে `caddy validate` দিয়ে যাচাই হবে, ভুল থাকলে আগের config থেকে যাবে।

**Databases**
- উপরে লেখা থাকবে: Host `nd-mysql` · Port `3306` (site-এর ভেতর থেকে)।
- Databases টেবিল (নাম, আকার, user) + **Add Database** (Database Name, Database User Name, Database User Password)।
- Database Users টেবিল + **Add Database User** (Database, User Name, Password, Permission: Read/Write বা Read-only)।
- প্রতিটা সারির action: Adminer-এ খোলা (SSO) · Export (.sql.gz) · Import · Delete।
- উপরে quota: "Databases: ১/৩ · DB size: 120MB (storage-এ গোনা হয়)"।

**Varnish Cache** → Phase 3-এ **"Cache"** নামে। ফিল্ড: Enable, Cache Lifetime (সেকেন্ড), Excluded Params, Excludes (URL তালিকা), Purge Entire Cache, নির্দিষ্ট URL purge। এটা Caddy cache-handler দিয়ে হবে। প্রথম ধাপে ট্যাবটা থাকবে না।

**SSL/TLS**
- টেবিল: Type (Let's Encrypt / Custom / Internal) · Domains · Expiration · Installed।
- Caddy নিজে থেকেই Let's Encrypt নেয়, তাই "New Let's Encrypt Certificate" ফর্মের দরকার নেই। তার বদলে domain alias তালিকা (Add Domain) থাকবে, আর প্রতিটা domain-এর DNS ঠিক আছে কিনা তার চেক।
- **Import Certificate** (certificate + private key + chain) রাখা হবে।

**Security**
- IP Blocking (Add IP), Bots Blocking (Add Bot, User-Agent দিয়ে), Basic Authentication (Enable, User Name, Password, Whitelisted IPs), Cloudflare-only traffic (Enable)।
- এগুলো সব Caddy label দিয়ে হবে: `remote_ip` matcher, `header User-Agent`, `basic_auth`, Cloudflare IP range।

**SSH/FTP → "Access"**
- Main Site User (Settings ট্যাবের সাথে মিল রেখে)।
- SFTP Users: Add User (User Name, Password, SSH Keys, Home Directory = htdocs-এর ভেতরের কোনো ফোল্ডার)। CloudPanel-এ SSH আর FTP আলাদা, আমাদের ক্ষেত্রে দুটোই **SFTPGo user**।
- আলাদাভাবে **"Web Terminal"** বাটন: browser-এ site container-এর shell, site UID হিসেবে (বিদ্যমান `ws_terminal.go`)।
- আসল SSH shell (port 22) প্রথম ধাপে থাকবে না। Web terminal আর SFTP দিয়েই বেশিরভাগ কাজ হয়ে যায়।

**File Manager**
- CloudPanel-এ বাঁদিকে folder tree আর ডানদিকে টেবিল (Name, Owner, Permissions, Size, Date), সাথে "Add New"।
- আমাদের বিদ্যমান workspace file manager-ই ব্যবহার হবে, শুধু htdocs-এ সীমাবদ্ধ থাকবে। নতুন যোগ হবে: permission (chmod) কলাম, zip/unzip, upload-এর আগে quota চেক (`CheckStorageQuota`)।

**Cron Jobs**
- তালিকা: Schedule · Command · শেষবার কখন চলেছে · ফলাফল (exit code) · Actions (Edit / Run now / Logs / Delete)।
- **Add Cron Job**: Template (Every minute, Every 5 minutes, Once an hour, Every 3 hours, Daily 00:00, Daily 4am, Weekly, Monthly) + Minute/Hour/Day/Month/Weekday + Command।
- আমাদের অতিরিক্ত: সর্বনিম্ন interval admin ঠিক করবে (quota)। তাই "Every minute" দেখা যাবে কিনা সেটা quota-র উপর নির্ভর করবে। প্রতিটা চলার log আর timeout থাকবে।

**Logs**
- Service (Caddy access / PHP error / Cron), Log File, Lines (10–10000), Update বাটন।
- আমাদের অতিরিক্ত: live tail (বিদ্যমান `ws_logs.go`)।

**Backups** (নতুন ট্যাব, CloudPanel-এ এটা শুধু admin-এর জন্য)
- Site ফাইল + DB dump, "Backup now", তালিকা, restore। Destination বিদ্যমান rclone settings থেকে।

### 9.6 Admin অংশে যা যোগ করতে হবে
| CloudPanel | NextDeploy-তে | যা করতে হবে |
|---|---|---|
| Dashboard (OS, hostname, IP, CPU/Disk/Mem/Load চার্ট) | Monitoring পেজ আছে | কিছু না |
| Users (username, email, first/last name, password, status, role, timezone, sites) | Team & Quotas | নতুন quota ফিল্ড: `max_sites`, `max_databases`, `max_cron_jobs`, `min_cron_interval` |
| Role: Site Manager | নেই | ঐচ্ছিক: একটা role যে সব site ম্যানেজ করতে পারবে, কিন্তু Admin Area দেখতে পাবে না |
| Events | Audit Trail | Site, DB, cron, SFTP-এর ঘটনাও audit-এ লেখা |
| Instance → Services (MySQL, NGINX, PHP-FPM 7.1…8.5, Redis, Varnish; Restart) | Containers পেজ | একটা "PHP Hosting Services" কার্ড: `nd-mysql`, `sftpgo`, `caddy`, প্রতিটা PHP version কয়টা site-এ চলছে, Restart (POST) |
| Instance → Settings (ProFTPD Masquerade Address, Timezone) | Settings | SFTP-এর public host/port |
| Backups (S3, Wasabi, DO Spaces, Dropbox, Google Drive, SFTP, Custom Rclone) | Backup পেজ আছে (rclone) | কিছু না |
| Security → Firewall rules, panel Basic Auth | নেই | পরে বিবেচনা করা যাবে (host firewall-এ হাত দিতে privileged helper লাগবে) |
| Settings → panel custom domain | Settings-এ আছে | কিছু না |

### 9.7 ডিজাইন স্টাইল
CloudPanel-এর লেআউট (হেডারে site-এর তথ্য, তার নিচে আড়াআড়ি ট্যাব, তারপর কার্ড আকারে সেকশন) আমাদের বিদ্যমান `AppWorkspace.tsx`-এর মতোই। তাই একই `Primitives.tsx` কম্পোনেন্ট আর বিদ্যমান থিম (dark mode সহ) ব্যবহার হবে। আলাদা কোনো স্টাইল লাগবে না।

---

## সূত্র
- লাইভ CloudPanel v2.5.4 instance (শুধু পড়ে দেখা, ২০২৬-১০-২৪)
- CloudPanel (per-site user + PHP-FPM pool): https://dreamserver.ro/en/blog/cloudpanel-free-server-control-panel/ · https://www.cloudpanel.io/docs/v1/frontend-area/domains
- EasyPanel vs CloudPanel (container PHP): https://www.cloudpanel.io/blog/easypanel-vs-cloudpanel/
- Enhance per-site containers: https://teklanhosting.co.uk/application · OpenPanel: https://openpanel.com/ · wpcontroller: https://docs.digitalocean.com/products/marketplace/catalog/wpcontroller
- Docker + cPanel conflict: https://www.inmotionhosting.com/blog/what-is-docker-vps-plans-container-workloads.md
- PHP-FPM pool per account: https://stackharbor.com/en/knowledge-base/cpweb-php-fpm-pool-per-account-vs-shared/
- Caddy + php-fpm in Docker: https://caddy.community/t/docker-stack-wordpress-mariadb-caddy-fastcgi/16919 · https://caddy.community/t/docker-reverse-proxy-for-multiple-php-sites/17732
- SFTPGo: https://github.com/drakkan/sftpgo (mirror: https://pkg.go.dev/github.com/drakkan/sftpgo)
