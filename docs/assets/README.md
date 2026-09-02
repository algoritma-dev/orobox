# Regenerating the README demo GIF

The demo GIF is recorded with [VHS](https://github.com/charmbracelet/vhs), from
[`demo.tape`](demo.tape), so it can be rebuilt instead of hand-edited.

```bash
# in a directory with an already-initialized bundle/project/demo (see the tape's own
# comment for why `orobox init` itself is not part of the recording)
vhs docs/assets/demo.tape
```

This writes `docs/assets/demo.gif`, which the README embeds directly below the badges.

If the asset does not exist yet, the README must not reference it — a broken image is
worse than no image. Track recording it with an issue ("Record README demo GIF") that
links to `demo.tape`.
