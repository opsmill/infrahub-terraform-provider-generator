import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Infrahub Terraform Provider Generator',
  tagline: 'Generate a Terraform provider for Infrahub from your GraphQL queries',
  favicon: 'img/favicon.ico',

  // Set the production url of your site here
  url: 'https://docs.infrahub.app',
  // Set the /<baseUrl>/ pathname under which your site is served
  baseUrl: '/',

  organizationName: 'opsmill',
  projectName: 'infrahub-terraform-provider-generator',

  onBrokenLinks: 'throw',
  onDuplicateRoutes: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },
  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang.
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          editUrl: 'https://github.com/opsmill/infrahub-terraform-provider-generator/tree/main/docs',
          routeBasePath: '/',
          sidebarCollapsed: true,
          sidebarPath: './sidebars.ts',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    navbar: {
      logo: {
        alt: 'Infrahub',
        src: 'img/infrahub-hori.svg',
        srcDark: 'img/infrahub-hori-dark.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'generatorSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          href: 'https://github.com/opsmill/infrahub-terraform-provider-generator',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
        },
      ],
    },
    footer: {
      copyright: `Copyright © ${new Date().getFullYear()} - <b>Infrahub</b> by OpsMill.`,
    },
    prism: {
      theme: prismThemes.oneDark,
      additionalLanguages: ['bash', 'go', 'graphql', 'json', 'toml', 'yaml'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
