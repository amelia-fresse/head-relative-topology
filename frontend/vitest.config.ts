import { defineConfig } from 'vitest/config'

// Separate from vite.config.ts so tests don't need the Wails typed-events plugin.
export default defineConfig({
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
    // themes.test.ts reads themes.css?raw; vitest blanks CSS unless included.
    css: { include: [/themes\.css/] },
  },
})
