// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
	site: 'https://docs.meshploy.com',
	build: {
		assets: 'assets',
	},
	integrations: [
		starlight({
			title: 'meshploy',
			description: 'Documentation for Meshploy, the open-source application platform for your own servers and private mesh.',
			logo: {
				src: './src/assets/logo.svg',
				replacesTitle: false,
				href: 'https://meshploy.com',
			},
			favicon: '/favicon.svg',
			components: {
				SiteTitle: './src/components/SiteTitle.astro',
				ThemeProvider: './src/components/ThemeProvider.astro',
				ThemeSelect: './src/components/ThemeSelect.astro',
			},
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/meshploy/meshploy' },
			],
			sidebar: [
				{
					label: 'Getting Started',
					items: [
						{ label: 'Introduction', slug: 'introduction' },
						{ label: 'Self-hosting', slug: 'self-hosting' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Deploy your first application', slug: 'guides/deploy-first-application' },
						{ label: 'Node roles & build placement', slug: 'guides/node-roles-and-build-placement' },
						{ label: 'Route an existing service', slug: 'guides/route-existing-service' },
						{ label: 'Domains & TLS', slug: 'guides/domains-and-tls' },
						{ label: 'Scoped agent access', slug: 'guides/scoped-agent-access' },
						{ label: 'Database backup & restore', slug: 'guides/database-backup-and-restore' },
					],
				},
				{
					label: 'Architecture',
					items: [
						{ label: 'How it works', slug: 'architecture/how-it-works' },
						{ label: 'Concepts & design decisions', slug: 'architecture/concepts' },
						{ label: 'Database schema', slug: 'architecture/database' },
					],
				},
				{
					label: 'CLI',
					items: [{ autogenerate: { directory: 'cli' } }],
				},
				{
					label: 'API',
					items: [{ autogenerate: { directory: 'api' } }],
				},
				{
					label: 'Contributing',
					items: [
						{ label: 'Contributing guide', slug: 'contributing/guide' },
						{ label: 'Security policy', slug: 'contributing/security' },
						{ label: 'Roadmap', slug: 'contributing/roadmap' },
					],
				},
			],
			customCss: ['./src/styles/custom.css'],
		}),
	],
});
