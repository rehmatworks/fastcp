<template>
    <div class="row">
        <div class="col-12">
            <div class="row mb-2">
                <div class="col-12">
                    <h4>FTP Management</h4>
                </div>
            </div>
            <div class="row">
                <div class="col-md-8">
                    <div class="card mb-3">
                        <div class="card-header bg-primary text-light">
                            FTP Connection Details
                        </div>
                        <div class="card-body">
                            <table class="table">
                                <tbody>
                                    <tr>
                                        <td style="width: 40%">FTP Host</td>
                                        <td>localhost</td>
                                    </tr>
                                    <tr>
                                        <td>FTP Port</td>
                                        <td>21</td>
                                    </tr>
                                    <tr>
                                        <td>Username</td>
                                        <td>{{ ftpUser }}</td>
                                    </tr>
                                    <tr>
                                        <td>Password</td>
                                        <td>
                                            <span v-if="!showPassword">********</span>
                                            <span v-else>{{ ftpPass }}</span>
                                            <small>
                                                <a
                                                    v-if="!showPassword"
                                                    @click="showPassword = true"
                                                    href="javascript:void(0)"
                                                    class="text-primary"
                                                >
                                                    <i class="fas fa-eye"></i> Show
                                                </a>
                                                <a
                                                    v-else
                                                    @click="showPassword = false"
                                                    href="javascript:void(0)"
                                                    class="text-primary"
                                                >
                                                    <i class="fas fa-eye-slash"></i> Hide
                                                </a>
                                                <a
                                                    @click="resetPassword()"
                                                    href="javascript:void(0)"
                                                    class="text-danger ml-2"
                                                >
                                                    <i class="fas fa-redo"></i> Reset Password
                                                </a>
                                            </small>
                                        </td>
                                    </tr>
                                    <tr>
                                        <td>Passive Mode Port Range</td>
                                        <td>30000-30009</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
                <div class="col-md-4">
                    <div class="card mb-3">
                        <div class="card-header bg-info text-light">
                            Quick Actions
                        </div>
                        <div class="card-body">
                            <div class="list-group">
                                <a href="#" class="list-group-item list-group-item-action">
                                    <i class="fas fa-key"></i> Change FTP Password
                                </a>
                                <a href="#" class="list-group-item list-group-item-action">
                                    <i class="fas fa-users"></i> Manage FTP Users
                                </a>
                                <a href="#" class="list-group-item list-group-item-action">
                                    <i class="fas fa-folder"></i> Browse Files
                                </a>
                            </div>
                        </div>
                    </div>
                    <div class="card">
                        <div class="card-header bg-success text-light">
                            FTP Status
                        </div>
                        <div class="card-body">
                            <div class="text-center">
                                <i class="fas fa-check-circle fa-3x text-success"></i>
                                <p class="mt-2 mb-0">FTP Server is running</p>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>

<script>
export default {
    data() {
        return {
            showPassword: false,
            ftpUser: 'ftpuser',
            ftpPass: 'ftppass'
        };
    },
    methods: {
        resetPassword() {
            // Find the current user's FTP user (from API) and call the detail reset_password action
            this.$store.commit('setBusy', true);

            // Get the list of FTP users for current account
            axios.get('/ftp/users/')
                .then((listRes) => {
                    const users = listRes.data || [];
                    if (!users.length) {
                        this.$store.commit('setBusy', false);
                        toastr.error('No FTP user found to reset.');
                        return;
                    }

                    // Use the first FTP user by default (adapt if you have multiple and a selection UI)
                    const user = users[0];
                    return axios.post(`/ftp/users/${user.id}/reset-password/`);
                })
                .then((res) => {
                    if (res && res.data) {
                        this.ftpPass = res.data.password;
                        this.showPassword = true;
                        toastr.success('Password has been reset successfully.');
                    }
                    this.$store.commit('setBusy', false);
                })
                .catch((err) => {
                    this.$store.commit('setBusy', false);
                    toastr.error('Password cannot be reset.');
                });
        }
    }
};
</script>
