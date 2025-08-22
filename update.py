from b2sdk.v2 import InMemoryAccountInfo, B2Api
import os

# When master branch is updated, this scripts pushes the latest ZIP package to
# CDN.

info = InMemoryAccountInfo()
b2_api = B2Api(info)
application_key_id = os.environ.get('B2_APP_KEY_ID')
application_key = os.environ.get('B2_APP_KEY')
b2_api.authorize_account('production', application_key_id, application_key)

package_path = os.path.join(os.getcwd(), './latest.zip')

bucket = b2_api.get_bucket_by_name('fastcp')
bucket.upload_local_file(
    local_file=package_path,
    file_name='latest.zip'
)
