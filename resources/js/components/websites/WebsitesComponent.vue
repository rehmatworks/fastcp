<template>
    <div class="row">
        <websites-table :pagination="true"/>
    </div>
</template>
<script>
import WebsitesTable from './WebsitesTable.vue'
export default {
    components: { WebsitesTable },
    
}
</script>