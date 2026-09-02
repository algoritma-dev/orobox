# Social preview

GitHub's social preview upload (Settings → General → Social preview) only accepts a
raster image (PNG/JPG, 1280×640 recommended), not SVG. Convert before uploading:

```bash
# any SVG rasterizer works; rsvg-convert is the smallest dependency
rsvg-convert -w 1280 -h 640 social-preview.svg -o social-preview.png
```

Upload is a manual step — see the manual settings checklist.
