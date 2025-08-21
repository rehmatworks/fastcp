from django.core.paginator import EmptyPage
from rest_framework import pagination
from rest_framework.response import Response


class FastcpPagination(pagination.PageNumberPagination):
    """Custom pagination class.
    
    To make the API consumption easier from our VueJS components, we have written
    this pagination class and here, we are returning just the page number, not the
    full URL.
    """
    def get_paginated_response(self, data):
        try:
            previous = self.page.previous_page_number()
        except EmptyPage:
            previous = None
        
        try:
            nextpage = self.page.next_page_number()
        except EmptyPage:
            nextpage = None
            
        return Response({
            'links': {
                'next': nextpage,
                'previous': previous
            },
            'count': self.page.paginator.count,
            'results': data
        })