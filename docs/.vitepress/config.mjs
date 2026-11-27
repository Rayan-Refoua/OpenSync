import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'OpenSync',
  description: 'OpenSync documentation',
  base: '/OpenSync/',
  cleanUrls: false,
  themeConfig: {
    search: { provider: 'local' },
    nav: [
      { text: 'Guide', link: '/guide/workflow' },
      { text: 'Engine', link: '/guide/engine' },
      { text: 'Reference', link: '/guide/reference' }
    ],
    sidebar: [
      { text: 'Start here', items: [
        { text: 'Overview', link: '/' },
        { text: 'Synchronization workflow', link: '/guide/workflow' },
        { text: 'Terms and evidence', link: '/guide/terms' }
      ] },
      { text: 'How OpenSync works', items: [
        { text: 'Engine stages', link: '/guide/engine' },
        { text: 'Timing and chronology', link: '/guide/timing' },
        { text: 'Projects and export', link: '/guide/projects' },
        { text: 'Desktop workstation', link: '/guide/desktop' },
        { text: 'Performance and I/O', link: '/guide/performance' }
      ] },
      { text: 'Development', items: [
        { text: 'Technology and code map', link: '/guide/technology' },
        { text: 'Testing and limitations', link: '/guide/testing' },
        { text: 'History and research', link: '/guide/history' },
        { text: 'Documentation reference', link: '/guide/reference' }
      ] }
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/Rayan-Refoua/OpenSync' }],
    footer: { message: 'OpenSync is under active development.' }
  }
})
