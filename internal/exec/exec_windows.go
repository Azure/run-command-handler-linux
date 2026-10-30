package exec

// O_CREATE|O_EXCL already rejects an existing final path, including a symlink.
const runAsNoFollow = 0
