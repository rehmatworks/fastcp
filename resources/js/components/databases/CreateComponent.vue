<template>
    <div class="row">
        <div class="col-12">
            <div class="card">
                <div class="card-header">
                    <h4 class="float-left mt-1">Create Database</h4>
                    <router-link
                        :to="{ name: 'databases' }"
                        class="btn mt-1 btn-sm float-right btn-warning"
                    >
                        <i class="fas fa-times"></i> Cancel
                    </router-link>
                </div>
                <div class="card-body">
                    <div class="row">
                        <div class="col-md-6">
                            <div v-if="$store.state.user && $store.state.user.is_root" class="form-group">
                                <label for="user">
                                    SSH User
                                    <small>
                                        <a @click="create=!create" href="javascript:void(0)" class="text-primary text-decoration-none">
                                            <span v-if="create">Cancel</span>
                                            <span v-else>
                                                <i class="fas fa-plus"></i> Create
                                            </span>
                                        </a>
                                    </small>
                                </label>
                                <usercreate-component v-if="create"/>
                                <v-select v-else :options="users" @search="getUsers" v-model="ssh_user"></v-select>
                                <p class="text-danger" v-if="!create && errors.user">{{ errors.user[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="name">Database Name</label>
                                <input
                                    id="name"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.name}"
                                    v-model="name"
                                    placeholder="Database name..."
                                />
                                <p class="invalid-feedback" v-if="errors.name">{{ errors.name[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="username">Database Username</label>
                                <input
                                    id="username"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.username}"
                                    v-model="username"
                                    placeholder="Database username..."
                                />
                                <p class="invalid-feedback" v-if="errors.username">{{ errors.username[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="password">User Password</label>
                                <input
                                    id="password"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.password}"
                                    v-model="password"
                                    placeholder="Database user password..."
                                />
                                <p class="invalid-feedback" v-if="errors.password">{{ errors.password[0] }}</p>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="card-footer">
                    <button @click="createDatabase()" class="btn btn-primary">Create</button>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default{
    data() {
        return {
            name: '',
            username: '',
            password: '',
            errors: {},
            create: false,
            ssh_user: '',
            users: []
        }
    },
    created() {
        const gen = this.genRandPassword || function(pwLen = 15) {
            const chars = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
            return Array(pwLen).fill(chars).map(c => c[Math.floor(Math.random() * c.length)]).join('');
        };
        this.password = gen();
        if(this.$store.state.user && this.$store.state.user.is_root) {
            this.getUsers();
        }
        const bus = (this.EventBus || window.EventBus);
        if (bus && typeof bus.$on === 'function') {
            bus.$on('userCreated', this.handleUserCreated);
        }
    },
    beforeDestroy() {
        const bus = (this.EventBus || window.EventBus);
        if (bus && typeof bus.$off === 'function') {
            bus.$off('userCreated', this.handleUserCreated);
        }
    },
    methods: {
        handleUserCreated(username) {
            this.create = false;
            this.ssh_user = username;
            this.getUsers();
        },
        getUsers(search='', loading=false) {
            let _this = this;
            _this.errors = {};
            if(loading) {
                loading(true);
            }
            _this.$store.commit('setBusy', true);
            axios.get(`/ssh-users/?q=${search}`).then((res) => {
                _this.$store.commit('setBusy', false);
                var users = [];
                for(var i = 0; i < res.data.results.length; i++) {
                    users.push(res.data.results[i].username);
                }
                if(loading) {
                    loading(false);
                }
                _this.users = users;
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                toastr.error('SSH users list cannot be obtained.');
                if(loading) {
                    loading(false);
                }
            });
        },
        createDatabase() {
            let _this = this;
            _this.errors = {};
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('name', _this.name);
            fd.append('ssh_user', _this.ssh_user);
            fd.append('username', _this.username);
            fd.append('password', _this.password);
            axios.post('/databases/', fd).then((res) => {
                _this.$store.commit('setBusy', false);
                toastr.success('Databases has been created successfully.');
                _this.$router.push({name: 'databases'});
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                _this.errors = err.response.data;
            });
        }
    },
    watch: {
        create(newval, oldval) {
            if(newval) {
                const gen = this.genRandPassword || function(pwLen = 15) {
                    const chars = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
                    return Array(pwLen).fill(chars).map(c => c[Math.floor(Math.random() * c.length)]).join('');
                };
                this.user_pass = gen();
                this.ssh_user = '';
            }
            this.errors = {};
        }
    }
};
</script>
