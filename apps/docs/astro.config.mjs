// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
	build: {
		assets: 'assets',
	},
	integrations: [
		starlight({
			title: 'Meshploy',
			description: 'Self-hosted PaaS built on WireGuard mesh — deploy across multi-cloud and bare metal.',
			logo: {
				src: './src/assets/logo.svg',
				replacesTitle: false,
				href: 'https://meshploy.com',
			},
			favicon: '/favicon.svg',
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
