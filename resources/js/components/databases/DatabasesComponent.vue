<template>
    <div class="row">
        <div class="col-12">
            <div class="row mb-4">
                <div class="col-12">
                    <div class="d-flex justify-content-between align-items-center">
                        <h4>Databases</h4>
                        <div>
                            <a :href="PMA_URL" target="_blank" class="btn btn-info mr-2">
                                <i class="fas fa-database"></i> Open phpMyAdmin
                            </a>
                            <router-link :to="{name: 'createdb'}" class="btn btn-primary">
                                <i class="fas fa-plus"></i> Create Database
                            </router-link>
                        </div>
                    </div>
                </div>
            </div>
            <databases-table :pagination="true"/>
        </div>
    </div>
</template>
<script>
import DatabasesTable from './DatabasesTable.vue'
export default {
    components: { DatabasesTable },
    computed: {
        PMA_URL() {
            try {
                // `PMA_URL` is injected into the page by `templates/master.html` as a global
                return window.PMA_URL || 'http://localhost:8081';
            } catch (e) {
                return 'http://localhost:8081';
            }
        }
    }
}
</script>