function ndGitAppId() {
  var r = document.getElementById('git-tab-root');
  return r && r.getAttribute('data-app-id') ? r.getAttribute('data-app-id').trim() : '';
}
var gitAppId = ndGitAppId();
    (function stripLegacyGitQueryParams() {
      try {
        var sp = new URLSearchParams(window.location.search);
        if (!sp.has('saved') && !sp.has('synced') && !sp.get('error')) return;
        sp.delete('saved');
        sp.delete('synced');
        sp.delete('error');
        var q = sp.toString();
        window.history.replaceState({}, '', window.location.pathname + (q ? '?' + q : '') + window.location.hash);
      } catch (e) {}
    })();
    function updateGitAuthMode() {
      var mode = document.getElementById('git-auth-mode-sel').value;
      var publicFields = document.getElementById('git-public-fields');
      var githubAppFields = document.getElementById('git-github-app-fields');
      var autoDeploySection = document.getElementById('git-auto-deploy-section');
      if (mode === 'github_app') {
        publicFields.classList.add('hidden');
        githubAppFields.classList.remove('hidden');
        if (autoDeploySection) autoDeploySection.classList.remove('hidden');
      } else {
        publicFields.classList.remove('hidden');
        githubAppFields.classList.add('hidden');
        if (autoDeploySection) autoDeploySection.classList.add('hidden');
      }
    }

    function onGitHubProviderChange() {
      var providerSelect = document.getElementById('git-provider-select');
      var picker = document.getElementById('git-repo-branch-picker');
      var msg = document.getElementById('git-picker-message');
      if (!providerSelect || providerSelect.value === '0') {
        if (picker) picker.classList.add('hidden');
        if (msg) msg.textContent = 'Select a provider to load repositories.';
        return;
      }
      if (picker) picker.classList.remove('hidden');
      loadGitProviderRepos(false);
    }

    function selectedGitProviderMeta() {
      var providerSelect = document.getElementById('git-provider-select');
      if (!providerSelect || providerSelect.value === '0') return null;
      var option = providerSelect.options[providerSelect.selectedIndex];
      return {
        id: providerSelect.value,
        provider: option ? option.dataset.provider : ''
      };
    }

    function setGitPickerMessage(text, isError) {
      var box = document.getElementById('git-picker-message');
      if (!box) return;
      box.textContent = text;
      box.className = 'rounded-lg border px-3 py-2 text-xs ' + (isError
        ? 'border-rose-500/25 bg-rose-500/10 text-rose-300'
        : 'border-border/50 bg-background/60 text-muted-foreground');
    }

    function loadGitProviderRepos(forceReload) {
      var meta = selectedGitProviderMeta();
      var repoSelect = document.getElementById('git-repo-select');
      if (!repoSelect) return;
      if (!meta || meta.provider !== 'github') {
        repoSelect.innerHTML = '<option value="">Select repository</option>';
        setGitPickerMessage('Select a GitHub provider first.', false);
        return;
      }
      if (!forceReload && repoSelect.dataset.loadedFor === meta.id) {
        return;
      }
      setGitPickerMessage('Loading repositories...', false);
      fetch('/apps/' + encodeURIComponent(gitAppId) + '/git/providers/' + encodeURIComponent(meta.id) + '/repos')
        .then(function(res) { return res.json().then(function(body) { return { ok: res.ok, body: body }; }); })
        .then(function(result) {
          if (!result.ok) throw new Error(result.body.error || 'Could not load repositories');
          var repos = result.body.repos || [];
          var currentURL = document.getElementById('git-repo-url-input').value;
          var currentRepo = currentURL.replace(/^https:\/\/github\.com\//, '').replace(/\.git$/, '');
          repoSelect.innerHTML = '<option value="">Select repository</option>';
          repos.forEach(function(repo) {
            var option = document.createElement('option');
            option.value = repo.full_name;
            option.textContent = repo.full_name;
            option.dataset.cloneUrl = repo.clone_url;
            option.dataset.defaultBranch = repo.default_branch || 'main';
            if (repo.full_name === currentRepo) option.selected = true;
            repoSelect.appendChild(option);
          });
          repoSelect.dataset.loadedFor = meta.id;
          if (repos.length === 0) {
            setGitPickerMessage('No accessible repositories. Grant repo access during GitHub App installation.', true);
          } else {
            setGitPickerMessage('Loaded ' + repos.length + ' repositories.', false);
            if (repoSelect.value) {
              applyGitRepoSelection();
            }
          }
        })
        .catch(function(err) {
          repoSelect.innerHTML = '<option value="">Select repository</option>';
          setGitPickerMessage(err.message || 'Could not load repositories.', true);
        });
    }

    function applyGitRepoSelection() {
      var repoSelect = document.getElementById('git-repo-select');
      var repoInput = document.getElementById('git-repo-url-input');
      if (!repoSelect || !repoInput) return;
      var option = repoSelect.options[repoSelect.selectedIndex];
      if (!option || !option.value) return;
      repoInput.value = option.dataset.cloneUrl || ('https://github.com/' + option.value + '.git');
      var branchInput = document.getElementById('git-branch-input');
      if (branchInput && !branchInput.value) {
        branchInput.value = option.dataset.defaultBranch || 'main';
      }
      loadGitProviderBranches(false);
    }

    function loadGitProviderBranches(forceReload) {
      var meta = selectedGitProviderMeta();
      var branchSelect = document.getElementById('git-branch-select');
      var repoSelect = document.getElementById('git-repo-select');
      if (!branchSelect) return;
      if (!meta || meta.provider !== 'github' || !repoSelect || !repoSelect.value) {
        branchSelect.innerHTML = '<option value="">Select branch</option>';
        return;
      }
      var cacheKey = meta.id + ':' + repoSelect.value;
      if (!forceReload && branchSelect.dataset.loadedFor === cacheKey) {
        return;
      }
      fetch('/apps/' + encodeURIComponent(gitAppId) + '/git/providers/' + encodeURIComponent(meta.id) + '/branches?repo=' + encodeURIComponent(repoSelect.value))
        .then(function(res) { return res.json().then(function(body) { return { ok: res.ok, body: body }; }); })
        .then(function(result) {
          if (!result.ok) throw new Error(result.body.error || 'Could not load branches');
          var branches = result.body.branches || [];
          var currentBranch = document.getElementById('git-branch-input').value;
          branchSelect.innerHTML = '<option value="">Select branch</option>';
          branches.forEach(function(branch) {
            var option = document.createElement('option');
            option.value = branch.name;
            option.textContent = branch.name;
            if (branch.name === currentBranch) option.selected = true;
            branchSelect.appendChild(option);
          });
          branchSelect.dataset.loadedFor = cacheKey;
        })
        .catch(function(err) {
          setGitPickerMessage(err.message || 'Could not load branches.', true);
          branchSelect.innerHTML = '<option value="">Select branch</option>';
        });
    }

    function applyGitBranchSelection() {
      var branchSelect = document.getElementById('git-branch-select');
      var branchInput = document.getElementById('git-branch-input');
      if (branchSelect && branchInput && branchSelect.value) {
        branchInput.value = branchSelect.value;
      }
    }

    updateGitAuthMode();
