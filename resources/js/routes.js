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
        path: '/websites/:id/files',
        name: 'filemanager',
        component: require('./components/filemanager/FilemanagerComponent').default,
        meta: {
            title: 'Manage Website Files'
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
        path: '/users',
        name: 'users',
        component: require('./components/users/UsersComponent').default,
        meta: {
            title: 'Manage Users'
        }
    },
    {
        path: '/users/:id',
        name: 'user',
        component: require('./components/users/UserComponent').default,
        meta: {
            title: 'Manage User'
        }
    },
    {
        path: '/create-user',
        name: 'createuser',
        component: require('./components/users/CreateComponent').default,
        meta: {
            title: 'Create User'
        }
    },
    {
        path: '/harware-info',
        name: 'hardware',
        component: require('./components/generic/HardwareComponent').default,
        meta: {
            title: 'Hardware Info'
        }
    },
    {
        path: '/ftp',
        name: 'ftp',
        component: require('./components/ftp/FtpComponent').default,
        meta: {
            title: 'FTP Management'
        }
    }
];