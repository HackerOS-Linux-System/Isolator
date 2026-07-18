project('isolator-store', 'vala', 'c',
  version: '0.1.0',
  license: 'Apache-2.0',
)

gnome = import('gnome')

dependencies = [
  dependency('gtk4'),
  dependency('libadwaita-1'),
  dependency('json-glib-1.0'),
  dependency('gee-0.8'),
  dependency('gio-2.0'),
  dependency('glib-2.0'),
  dependency('gobject-2.0'),
]

resources = gnome.compile_resources(
  'store-resources',
  'data' / 'store.gresource.xml',
  source_dir: 'data',
  c_name: 'store',
)

executable(
  'isolator-store',
  sources: [
    'src/main.vala',
    'src/isolator-client.vala',
    'src/package.vala',
    'src/window.vala',
    'src/package-row.vala',
    resources,
  ],
  dependencies: dependencies,
  install: true,
)
