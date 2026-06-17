import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  generatorSidebar: [
    'readme',
    {
      type: 'category',
      label: 'Tutorials',
      items: [
        'tutorials/getting-started',
      ],
    },
    {
      type: 'category',
      label: 'How-to Guides',
      items: [
        'guides/graphql-file-layout',
        'guides/schema-typing',
        'guides/sdk-bindings',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/cli',
        'reference/architecture',
      ],
    },
  ],
};

export default sidebars;
