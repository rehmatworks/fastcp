export const routes = [
    {
        path: '/',
        name: 'dashboard',
        component: require('./components/dashboard/DashboardComponent').default,
        meta: {
            title: 'Dashboard Home'
        }
    },
    {
        path: '/websites',
        name: 'websites',
        component: require('./components/websites/WebsitesComponent').default,
        meta: {
            title: 'Manage Websites'
        }
    },
    {
        path: '/websites/:id',
        name: 'website',
        component: require('./components/websites/WebsiteComponent').default,
        meta: {
            title: 'Manage Website'
        }
    },
    {
        path: '/deploy-website',
        name: 'deploysite',
        component: require('./components/websites/CreateComponent').default,
        meta: {
            title: 'Create Website'
        }
    },
    {
        path: '/databases',
        name: 'databases',
        component: require('./components/databases/DatabasesComponent').default,
        meta: {
            title: 'Manage Databases'
        }
    },
    {
        path: '/databases/:id',
        name: 'database',
        component: require('./components/databases/DatabaseComponent').default,
        meta: {
            title: 'Manage Database'
        }
    },
    {
        path: '/create-database',
        name: 'createdb',
        component: require('./components/databases/CreateComponent').default,
        meta: {
            title: 'Create Database'
        }
    },
    {
        path: '/file-manager',
        name: 'files',
        component: require('./components/filemanager/FilemanagerComponent').default,
        meta: {
            title: 'File Manager'
        }
    },
    {
        path: '/users',
        name: 'users',
        component: require('./components/users/UsersComponent').default,
        meta: {
            title: 'Manage Users'
        }
    },
    {
        path: '/harware-info',
        name: 'hardware',
        component: require('./components/generic/HardwareComponent').default,
        meta: {
            title: 'Hardware Info'
        }
    }
];